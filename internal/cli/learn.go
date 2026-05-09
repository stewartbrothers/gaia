package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	agentguide "github.com/stewartbrothers/gaia"
	"github.com/stewartbrothers/gaia/internal/version"
)

// learnResult is the JSON envelope payload for `gaia learn --format json`.
// Mirrors what an MCP resource for the same content would carry: the
// raw markdown plus enough metadata for an agent to decide whether to
// re-fetch (length is a cheap drift signal) or know which gaia
// version produced it.
type learnResult struct {
	Content string `json:"content"`
	Length  int    `json:"length"`
	Version string `json:"version"`
}

// newLearnCmd registers `gaia learn`, which prints the embedded
// agent-onboarding guide.
//
// `gaia learn` inverts the normal output default: bare invocation
// returns raw markdown so `gaia learn | less` (or piping into a chat
// context) is the obvious thing. `--format json` switches to the
// standard envelope shape ({schema_version, data: {content, length,
// version}}) for MCP-style consumers that want metadata alongside
// the content.
//
// The content comes from agentguide.Markdown, which is //go:embed'd
// from docs/agent-guide.md at build time. The agentguide unit test
// asserts byte-equality with the source file so the embedded copy
// never drifts.
func newLearnCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "learn",
		Short: "Print the embedded agent-onboarding guide",
		Long: `Prints the canonical agent-onboarding briefing for AI coding agents
using gaia. Default output is the raw markdown (suitable for piping
into a chat context or rendering with a markdown viewer);
--format json returns the same content inside the standard gaia
envelope as {schema_version, data: {content, length, version}}.

The content is //go:embed'd from docs/agent-guide.md at build time —
the file you see is byte-identical to the doc in the repo.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// `gaia learn` inverts the default-format convention: if
			// the user didn't pass --format explicitly, emit raw
			// markdown rather than a JSON envelope. The envelope
			// shape is opt-in via --format json so MCP-style consumers
			// who want the metadata can ask for it.
			//
			// cmd.Flags().Changed("format") is false when the user
			// didn't set the persistent flag; the global default is
			// "json", so without this special-case `gaia learn` would
			// always wrap. Persistent flags inherit from the root, so
			// we read via cmd.Flags() (which sees inherited persistent
			// flags), not just the local cmd.LocalFlags().
			format, _ := cmd.Flags().GetString("format")
			explicitFormat := cmd.Flags().Changed("format")
			flags.Format = format

			if explicitFormat && format == "json" {
				data := learnResult{
					Content: agentguide.Markdown,
					Length:  len(agentguide.Markdown),
					Version: version.Version,
				}
				return renderEnvelope(cmd, flags, data, nil, prettyLearn)
			}
			// Bare `gaia learn`, `--format pretty`, or any non-json
			// explicit format: write the markdown verbatim. The guide
			// ships with its own structure — wrapping it in further
			// pretty rendering would be noise.
			_, err := fmt.Fprint(cmd.OutOrStdout(), agentguide.Markdown)
			return err
		},
	}
}

// prettyLearn writes the markdown content of a learnResult to w.
// Reached only when the operator explicitly asked for pretty
// rendering on top of an envelope (rare for `learn`, but kept for
// completeness with the renderEnvelope contract).
func prettyLearn(w io.Writer, data any) error {
	r, ok := data.(learnResult)
	if !ok {
		return fmt.Errorf("prettyLearn: unexpected type %T", data)
	}
	_, err := fmt.Fprint(w, r.Content)
	return err
}
