package cli

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// defaultCIWaitInterval is how often `gaia pr ci-wait` polls the
// PR's commit status when --interval isn't supplied. 10s matches
// the rough cadence forge UIs poll at; cheap on the upstream and
// fine-grained enough that a check that finishes mid-window shows
// up in the next iteration.
const defaultCIWaitInterval = 10 * time.Second

// defaultCIWaitTimeout is how long ci-wait will keep polling
// before giving up with check_flaky. 30 minutes covers most
// reasonable CI suites; chains override per-step via
// --timeout / chain timeout: knobs.
const defaultCIWaitTimeout = 30 * time.Minute

// flakyMarkerRE matches check names that announce themselves as
// retry/flaky shapes (e.g. "tests (attempt 2/3)", "flaky-rerun",
// "ci-flaky"). Hits this → the check goes into the flaky bucket
// rather than the failed bucket, so a chain's
// `yield_on: [check_flaky]` catches it instead of
// `abort_on: [check_failed]` triggering. The pattern is
// intentionally narrow so a real test failure isn't accidentally
// classified as flaky.
var flakyMarkerRE = regexp.MustCompile(`(?i)(\bflaky\b|\battempt\s*\d+\b|\bretry\s*\d*\b|\bretries\b|\brerun\b)`)

func newPRCIWaitCmd(flags *globalFlags) *cobra.Command {
	var (
		timeout    time.Duration
		interval   time.Duration
		flakyExtra []string
	)
	cmd := &cobra.Command{
		Use:   "ci-wait <number>",
		Short: "Block until a PR's CI checks finish (with structured exits for chain routing)",
		Long: `Poll the PR's commit-status / check-runs endpoint until
all checks have completed or --timeout is reached.

Designed for chain consumption — exits with structured codes the
chain runtime maps to yield_on / abort_on conditions:

  0   all checks succeeded
  10  CheckFailed   — at least one non-flaky check failed
                      (chains: typically abort_on: [check_failed])
  11  CheckFlaky    — only flaky / retryable failures seen,
                      OR --timeout reached while still pending
                      (chains: typically yield_on: [check_flaky])

A check is classified "flaky" when its name matches the
retry-marker regex (` + "`flaky`" + ` / ` + "`attempt N`" + ` /
` + "`retry`" + ` / ` + "`rerun`" + `, case-insensitive). Use
--flaky-marker to add additional substrings (case-insensitive)
that should also count as flaky.

Output: a single envelope carrying the final CI summary +
per-check name/state pairs, on stdout. Stderr stays quiet unless
--verbose is set on the parent command.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueNumber(args[0])
			if err != nil {
				return err
			}
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}

			summary, classifyErr := waitForCI(cmd.Context(), p, owner, repo, n, ciWaitOptions{
				Timeout:    timeout,
				Interval:   interval,
				FlakyExtra: flakyExtra,
			})

			// We always render the envelope (even on classifyErr) so
			// the agent gets the per-check breakdown alongside the
			// exit code — it's the actionable detail.
			if summary != nil {
				if renderErr := renderEnvelope(cmd, flags, summary, nil, nil); renderErr != nil {
					return renderErr
				}
			}
			return classifyErr
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", defaultCIWaitTimeout, "give up after this duration → exit code CheckFlaky")
	cmd.Flags().DurationVar(&interval, "interval", defaultCIWaitInterval, "poll interval")
	cmd.Flags().StringSliceVar(&flakyExtra, "flaky-marker", nil, "additional case-insensitive substring(s) marking a check as flaky (repeatable)")
	return cmd
}

// ciWaitOptions tunes waitForCI. Defaults match the user-visible
// flag defaults so the CLI is the only knob — nothing else
// constructs ciWaitOptions today.
type ciWaitOptions struct {
	Timeout    time.Duration
	Interval   time.Duration
	FlakyExtra []string
}

// waitForCI polls until checks settle and returns the final
// CISummary plus a structured error mapping to one of:
//
//	nil                      — all checks succeeded
//	exitcode.CheckFailed     — non-flaky failure
//	exitcode.CheckFlaky      — flaky-only failure OR timeout
//	other (Auth/NotFound/…)  — provider error during polling
//
// The summary is always populated when there's anything to show
// (could be partial if the provider errored on the first poll;
// that case returns nil + the underlying error).
func waitForCI(ctx context.Context, p provider.Provider, owner, repo string, n int, opts ciWaitOptions) (*types.CISummary, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = defaultCIWaitTimeout
	}
	if opts.Interval <= 0 {
		opts.Interval = defaultCIWaitInterval
	}

	deadline := time.Now().Add(opts.Timeout)
	var last *types.CISummary
	for {
		pr, err := p.GetPullRequest(ctx, owner, repo, n, provider.GetPullRequestOptions{WithCISummary: true})
		if err != nil {
			return last, err
		}
		last = pr.CISummary
		if last == nil {
			// No CI configured at all — treat as success.
			return &types.CISummary{State: "success"}, nil
		}

		if last.State != "pending" {
			// Settled. Classify failures.
			return last, classifyChecks(last, opts.FlakyExtra)
		}

		if time.Now().After(deadline) {
			// Timeout while pending → flaky (caller is expected to
			// wait + retry, which is exactly what yield_on: [check_flaky]
			// → resume gives them).
			return last, exitcode.Errorf(exitcode.CheckFlaky,
				"ci-wait: timed out after %s with checks still pending (%d/%d)",
				opts.Timeout, last.Pending, last.Total)
		}

		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(opts.Interval):
		}
	}
}

// classifyChecks decides between success / CheckFlaky / CheckFailed
// once the summary has settled (no more pending checks).
func classifyChecks(s *types.CISummary, flakyExtra []string) error {
	if s.Failed == 0 {
		return nil
	}
	// Any non-flaky-named failure → CheckFailed.
	// All failed checks named flaky → CheckFlaky.
	flakyOnly := true
	failingNames := make([]string, 0, s.Failed)
	for _, c := range s.Checks {
		if !isFailureState(c.State) {
			continue
		}
		failingNames = append(failingNames, c.Name)
		if !isFlakyName(c.Name, flakyExtra) {
			flakyOnly = false
		}
	}
	// Defensive: if we don't have per-check names (provider didn't
	// populate them), fall back to CheckFailed — we can't prove
	// flakiness without names, and a hard fail should never be
	// silently demoted.
	if len(failingNames) == 0 {
		return exitcode.Errorf(exitcode.CheckFailed,
			"ci-wait: %d check(s) failed (no per-check names available)", s.Failed)
	}
	if flakyOnly {
		return exitcode.Errorf(exitcode.CheckFlaky,
			"ci-wait: %d flaky-named check(s) failed: %s", s.Failed, strings.Join(failingNames, ", "))
	}
	return exitcode.Errorf(exitcode.CheckFailed,
		"ci-wait: %d check(s) failed: %s", s.Failed, strings.Join(failingNames, ", "))
}

// isFailureState reports whether a per-check state value indicates
// a real failure (vs success / pending / skipped). Mirrors the
// vocabulary toCISummary uses on both providers.
func isFailureState(state string) bool {
	switch strings.ToLower(state) {
	case "failure", "error", "timed_out", "cancelled", "action_required", "stale":
		return true
	}
	return false
}

// isFlakyName reports whether a check name suggests it's a known
// flaky / retry-marker shape. Built-in regex covers the common
// patterns; flakyExtra adds operator-specified substrings.
func isFlakyName(name string, flakyExtra []string) bool {
	if flakyMarkerRE.MatchString(name) {
		return true
	}
	low := strings.ToLower(name)
	for _, m := range flakyExtra {
		if strings.Contains(low, strings.ToLower(m)) {
			return true
		}
	}
	return false
}
