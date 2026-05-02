package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func newPRReviewCmd(flags *globalFlags) *cobra.Command {
	var (
		state    string
		body     string
		comments []string
		dryRun   bool
	)
	cmd := &cobra.Command{
		Use:   "review <number>",
		Short: "Submit a review with state (approve / request-changes / comment) and optional inline comments",
		Long: `Submits a PR review.

  $ gaia pr review 42 --state approve --body "ship it"

  $ gaia pr review 42 --state request-changes \
      --body "few asks below" \
      --comment "core/x.go:42:rename this" \
      --comment "core/y.go:18:tighten loop"

--state values: approve | request-changes | comment.
--comment format: path:line:body (path may not contain ':'). Repeatable.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueNumber(args[0])
			if err != nil {
				return err
			}
			event, err := stateToReviewEvent(state)
			if err != nil {
				return err
			}
			b, err := readBody(cmd.InOrStdin(), body)
			if err != nil {
				return err
			}
			parsed, err := parseInlineComments(comments)
			if err != nil {
				return err
			}

			opts := provider.SubmitReviewOptions{
				Event:    event,
				Body:     b,
				Comments: parsed,
			}

			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if dryRun {
				return printDryRun(cmd, fmt.Sprintf("POST /repos/%s/%s/pulls/%d/reviews", owner, repo, n), opts)
			}
			if err := p.SubmitReview(cmd.Context(), owner, repo, n, opts); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Submitted %s review on #%d\n", state, n)
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "review state: approve | request-changes | comment (required)")
	cmd.Flags().StringVar(&body, "body", "", "top-level review body, or \"-\" for stdin")
	cmd.Flags().StringSliceVar(&comments, "comment", nil, "inline comment as path:line:body (repeatable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the request without submitting")
	return cmd
}

func stateToReviewEvent(state string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "approve", "approved":
		return "APPROVED", nil
	case "request-changes", "request_changes", "changes":
		return "REQUEST_CHANGES", nil
	case "comment":
		return "COMMENT", nil
	case "":
		return "", exitcode.Errorf(exitcode.Usage, "--state is required (approve | request-changes | comment)")
	default:
		return "", exitcode.Errorf(exitcode.Usage, "--state must be approve|request-changes|comment; got %q", state)
	}
}

// parseInlineComments turns each "path:line:body" string into a
// ReviewInlineComment. Path is the substring up to the first colon;
// line is the next number; body is everything else (so it may contain
// any other character including colons).
func parseInlineComments(raws []string) ([]provider.ReviewInlineComment, error) {
	out := make([]provider.ReviewInlineComment, 0, len(raws))
	for _, raw := range raws {
		c, err := parseOneInlineComment(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func parseOneInlineComment(raw string) (provider.ReviewInlineComment, error) {
	first := strings.IndexByte(raw, ':')
	if first < 0 {
		return provider.ReviewInlineComment{}, exitcode.Errorf(exitcode.Usage,
			"--comment must be path:line:body; got %q", raw)
	}
	path := raw[:first]
	rest := raw[first+1:]
	second := strings.IndexByte(rest, ':')
	if second < 0 {
		return provider.ReviewInlineComment{}, exitcode.Errorf(exitcode.Usage,
			"--comment must include a body after path:line; got %q", raw)
	}
	lineStr := rest[:second]
	body := rest[second+1:]
	line, err := strconv.Atoi(lineStr)
	if err != nil || line < 1 {
		return provider.ReviewInlineComment{}, exitcode.Errorf(exitcode.Usage,
			"--comment line must be a positive integer; got %q from %q", lineStr, raw)
	}
	if path == "" || body == "" {
		return provider.ReviewInlineComment{}, exitcode.Errorf(exitcode.Usage,
			"--comment requires non-empty path and body; got %q", raw)
	}
	return provider.ReviewInlineComment{Path: path, Line: line, Body: body}, nil
}
