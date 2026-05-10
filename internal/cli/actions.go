package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func newActionsCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "actions",
		Short: "List, view, and manage Actions workflow runs",
		Long: `Inspect Forgejo Actions workflow runs: list recent runs and
view a run's jobs.

The <run-id> arguments accept the user-facing run number from the
UI URL (the integer in /actions/runs/362), not the internal database
ID — gaia resolves it transparently.

  gaia actions list                     # recent runs (all statuses)
  gaia actions list --status failure    # only failed runs
  gaia actions view <run-id>            # status + summary
  gaia actions view <run-id> --with-jobs # include job list
  gaia actions logs <run-id>            # currently unsupported on Forgejo v15 (#266)
  gaia actions rerun <run-id>           # currently unsupported on Forgejo v15 (#267)

Forgejo v15.0.1 does not expose a logs or rerun API endpoint;
both commands return a clear unsupported error with the run's UI
URL so you can grab logs or re-trigger from the browser.`,
	}
	cmd.AddCommand(newActionsListCmd(flags))
	cmd.AddCommand(newActionsViewCmd(flags))
	cmd.AddCommand(newActionsLogsCmd(flags))
	cmd.AddCommand(newActionsRerunCmd(flags))
	return cmd
}

func newActionsListCmd(flags *globalFlags) *cobra.Command {
	var (
		status string
		branch string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent workflow runs (newest first)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if flags.Format == "ndjson" {
				return renderListStreaming(cmd, flags, func(cursor string) ([]any, *provider.Page, error) {
					runs, page, err := p.ListWorkflowRuns(cmd.Context(), owner, repo, provider.ListWorkflowRunsOptions{
						Status: status,
						Branch: branch,
						Limit:  flags.Limit,
						Cursor: cursor,
					})
					if err != nil {
						return nil, nil, err
					}
					return toAnySlice(runs), page, nil
				})
			}
			runs, page, err := p.ListWorkflowRuns(cmd.Context(), owner, repo, provider.ListWorkflowRunsOptions{
				Status: status,
				Branch: branch,
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, runs, page, prettyRunList)
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "filter by status: waiting, running, success, failure, cancelled")
	cmd.Flags().StringVar(&branch, "branch", "", "filter by branch name")
	return cmd
}

func newActionsViewCmd(flags *globalFlags) *cobra.Command {
	var withJobs bool
	cmd := &cobra.Command{
		Use:   "view <run-id>",
		Short: "View one workflow run (status, branch, commit)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID, err := parseRunID(args[0])
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
			run, err := p.GetWorkflowRun(cmd.Context(), owner, repo, runID, provider.GetWorkflowRunOptions{
				WithJobs: withJobs,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, run, nil, prettyRunView)
		},
	}
	cmd.Flags().BoolVar(&withJobs, "with-jobs", false, "inline the list of jobs (steps included)")
	return cmd
}

func newActionsLogsCmd(flags *globalFlags) *cobra.Command {
	var failedOnly bool
	cmd := &cobra.Command{
		Use:   "logs <run-id>",
		Short: "Print job logs for a run (in JSON mode: array of per-job log objects)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID, err := parseRunID(args[0])
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
			logs, err := p.GetWorkflowRunLogs(cmd.Context(), owner, repo, runID, provider.GetWorkflowRunLogsOptions{
				FailedOnly: failedOnly,
			})
			if err != nil {
				return err
			}
			// In JSON mode return the standard envelope wrapping the
			// []WorkflowRunLogs slice. In pretty mode print as plain text,
			// job by job, so it's paste-friendly.
			if flags.Format == "json" {
				return renderEnvelope(cmd, flags, logs, nil, nil)
			}
			// pretty / ndjson → plain text lines.
			return prettyLogs(cmd.OutOrStdout(), logs)
		},
	}
	cmd.Flags().BoolVar(&failedOnly, "failed-only", false, "only retrieve logs from failed jobs")
	return cmd
}

func newActionsRerunCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "rerun <run-id>",
		Short: "Re-trigger a workflow run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID, err := parseRunID(args[0])
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
			if err := p.RerunWorkflowRun(cmd.Context(), owner, repo, runID); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Re-triggered run %d\n", runID)
			return nil
		},
	}
}

// parseRunID parses a string as a workflow run ID (positive int64).
func parseRunID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, exitcode.Errorf(exitcode.Usage, "run-id must be a positive integer; got %q", s)
	}
	return id, nil
}

// prettyRunList renders a []WorkflowRun as a tab-separated table.
//
// ID is the user-facing run number (matches the integer in the UI URL,
// e.g. /actions/runs/362). RunID is the internal Forgejo database ID
// the API needs for follow-up reads. Both are surfaced because agents
// debugging from a UI URL want the first, while agents calling further
// API endpoints want the second.
func prettyRunList(w io.Writer, data any) error {
	runs, ok := data.([]types.WorkflowRun)
	if !ok {
		return fmt.Errorf("prettyRunList: unexpected type %T", data)
	}
	if len(runs) == 0 {
		_, _ = fmt.Fprintln(w, "(no workflow runs)")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tWORKFLOW\tSTATUS\tBRANCH\tACTOR\tUPDATED")
	for _, r := range runs {
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
			r.ID,
			truncate(r.WorkflowName, 30),
			r.Status,
			truncate(r.Branch, 30),
			r.Actor.Login,
			r.UpdatedAt.Format("2006-01-02 15:04"),
		)
	}
	return tw.Flush()
}

// prettyRunView renders a *WorkflowRun detail block.
func prettyRunView(w io.Writer, data any) error {
	run, ok := data.(*types.WorkflowRun)
	if !ok {
		return fmt.Errorf("prettyRunView: unexpected type %T", data)
	}
	_, _ = fmt.Fprintf(w, "Run #%d — %s\n", run.ID, run.WorkflowName)
	_, _ = fmt.Fprintf(w, "  Event:      %s\n", run.Event)
	_, _ = fmt.Fprintf(w, "  Status:     %s\n", run.Status)
	_, _ = fmt.Fprintf(w, "  Branch:     %s\n", run.Branch)
	_, _ = fmt.Fprintf(w, "  SHA:        %s\n", run.HeadSHA)
	_, _ = fmt.Fprintf(w, "  Actor:      %s\n", run.Actor.Login)
	_, _ = fmt.Fprintf(w, "  Created:    %s\n", run.CreatedAt.Format("2006-01-02 15:04"))
	_, _ = fmt.Fprintf(w, "  Updated:    %s\n", run.UpdatedAt.Format("2006-01-02 15:04"))
	if run.HTMLURL != "" {
		_, _ = fmt.Fprintf(w, "  URL:        %s\n", run.HTMLURL)
	}
	if run.HeadMessage != "" {
		_, _ = fmt.Fprintln(w)
		writeExternal(w, run.HeadMessage)
	}
	if len(run.Jobs) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "Jobs:")
		for _, j := range run.Jobs {
			_, _ = fmt.Fprintf(w, "  [%s] %s\n", j.Status, j.Name)
		}
	}
	return nil
}

// prettyLogs writes per-job log lines to w in a human-readable format.
func prettyLogs(w io.Writer, logs []types.WorkflowRunLogs) error {
	if len(logs) == 0 {
		_, _ = fmt.Fprintln(w, "(no logs)")
		return nil
	}
	for _, jl := range logs {
		_, _ = fmt.Fprintf(w, "=== Job %d: %s ===\n", jl.JobID, jl.JobName)
		for _, line := range jl.Lines {
			_, _ = fmt.Fprintln(w, line)
		}
		if !strings.HasSuffix(jl.JobName, "\n") {
			_, _ = fmt.Fprintln(w)
		}
	}
	return nil
}
