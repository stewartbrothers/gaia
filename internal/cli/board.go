package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// Kanban boards and the Do Now flag are Aikestra concepts (#404). The
// commands are gated by CapBoards / CapIssuePriority, so on Forgejo and
// GitHub they stop at the capability guard. Boards belong to the owner
// (an Aikestra Project); --repo's repo half is ignored.

func newBoardCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "board",
		Short: "List kanban boards and view their stages (Aikestra)",
		Long: `List kanban boards and view their stages and issue queues.

Boards belong to the owner (an Aikestra Project), so only the owner half
of --repo matters. Move an issue between stages with ` + "`gaia issue move`" + `.`,
	}
	cmd.AddCommand(newBoardListCmd(flags))
	cmd.AddCommand(newBoardViewCmd(flags))
	return cmd
}

func newBoardListCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the project's boards and their stages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ops, err := buildBoardOps(flags)
			if err != nil {
				return err
			}
			owner, _, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			boards, page, err := ops.ListBoards(cmd.Context(), owner, provider.ListBoardsOptions{
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, boards, page, prettyBoardList)
		},
	}
}

func newBoardViewCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "view <board>",
		Short: "Show a board's stages with their open issues in queue order",
		Long: `Show a board's stages with their open issues in queue order.

<board> is the board's name (case-insensitive) or ID.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ops, err := buildBoardOps(flags)
			if err != nil {
				return err
			}
			owner, _, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			board, err := ops.GetBoard(cmd.Context(), owner, args[0])
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, board, nil, prettyBoardView)
		},
	}
}

func newIssueMoveCmd(flags *globalFlags) *cobra.Command {
	var opts provider.MoveIssueOptions
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "move <number> --stage <stage>",
		Short: "Move an issue to another board stage (Aikestra)",
		Long: `Move an issue to another stage, optionally on another board.

--stage and --board take a name (case-insensitive) or ID; without
--board the issue stays on its current board. While an open issue blocks
this one, the move is refused (exit 9, PolicyViolation) and the error
names the blocking issues — close them, or remove the dependency with
` + "`gaia issue dep remove`" + `.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueNumber(args[0])
			if err != nil {
				return err
			}
			if strings.TrimSpace(opts.Stage) == "" {
				return exitcode.Errorf(exitcode.Usage, "--stage is required")
			}
			ops, err := buildBoardOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if dryRun {
				return printDryRun(cmd, fmt.Sprintf("PUT /projects/%s/issues/%d/placement", owner, n), opts)
			}
			issue, err := ops.MoveIssue(cmd.Context(), owner, repo, n, opts)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, issue, nil, prettyIssueView)
		},
	}
	cmd.Flags().StringVar(&opts.Stage, "stage", "", "target stage name or ID (required)")
	cmd.Flags().StringVar(&opts.Board, "board", "", "target board name or ID (default: the issue's current board)")
	cmd.Flags().StringVar(&opts.Note, "note", "", "why, recorded with the move (required when the Project requires notes)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the request and exit without moving")
	return cmd
}

func newIssuePriorityCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "priority <number> do-now|normal",
		Short: "Set or clear an issue's Do Now priority (Aikestra)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueNumber(args[0])
			if err != nil {
				return err
			}
			var doNow bool
			switch strings.ToLower(args[1]) {
			case "do-now":
				doNow = true
			case "normal":
			default:
				return exitcode.Errorf(exitcode.Usage, "priority must be do-now or normal, got %q", args[1])
			}
			ops, err := buildIssuePriorityOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			issue, err := ops.SetIssuePriority(cmd.Context(), owner, repo, n, doNow)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, issue, nil, prettyIssueView)
		},
	}
}

func buildBoardOps(flags *globalFlags) (provider.BoardOps, error) {
	p, _, err := buildForgejoProvider(flags)
	return p, err
}

func buildIssuePriorityOps(flags *globalFlags) (provider.IssuePriorityOps, error) {
	p, _, err := buildForgejoProvider(flags)
	return p, err
}

func prettyBoardList(w io.Writer, data any) error {
	boards, ok := data.([]types.Board)
	if !ok {
		return fmt.Errorf("prettyBoardList: unexpected type %T", data)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tDEFAULT\tSTAGES")
	for _, b := range boards {
		names := make([]string, len(b.Stages))
		for i, s := range b.Stages {
			names[i] = s.Name
		}
		_, _ = fmt.Fprintf(tw, "%s\t%v\t%s\n", b.Name, b.Default, strings.Join(names, " → "))
	}
	return tw.Flush()
}

func prettyBoardView(w io.Writer, data any) error {
	b, ok := data.(*types.Board)
	if !ok {
		return fmt.Errorf("prettyBoardView: unexpected type %T", data)
	}
	header := b.Name
	if b.Paused {
		header += " (paused)"
	}
	_, _ = fmt.Fprintln(w, header)
	for _, s := range b.Stages {
		queue := make([]string, len(s.Issues))
		for i, n := range s.Issues {
			queue[i] = fmt.Sprintf("#%d", n)
		}
		name := s.Name
		if s.Paused {
			name += " (paused)"
		}
		_, _ = fmt.Fprintf(w, "  %s: %s\n", name, strings.Join(queue, " "))
	}
	return nil
}

// writeAikestraFields adds the typed-issue and board lines to the pretty
// issue view. Forgejo and GitHub issues leave the fields zero, so
// nothing is printed for them.
func writeAikestraFields(w io.Writer, issue *types.Issue) {
	var parts []string
	if issue.Type != "" {
		parts = append(parts, "Type: "+issue.Type)
	}
	if issue.DoNow {
		parts = append(parts, "Priority: Do Now")
	}
	if issue.Board != "" || issue.Stage != "" {
		parts = append(parts, fmt.Sprintf("Stage: %s / %s", issue.Board, issue.Stage))
	}
	if issue.Parent != 0 {
		parts = append(parts, fmt.Sprintf("Parent: #%d", issue.Parent))
	}
	if len(parts) > 0 {
		_, _ = fmt.Fprintln(w, strings.Join(parts, "   "))
	}
}
