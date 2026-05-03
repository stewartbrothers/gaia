package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync/atomic"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/envelope"
	"github.com/stewartbrothers/gaia/core/provider"
)

// externalMarkersEnabled is read by writeExternal at pretty-render
// time to decide whether to wrap forge-supplied user content in
// <<<EXTERNAL / EXTERNAL>>> delimiters. Default ON (1); the
// --no-external-markers flag flips it to 0 before pretty renderers
// are invoked.
//
// atomic.Int32 (rather than a plain bool) so concurrent CLI
// invocations from separate root commands inside the same process
// (tests, primarily) don't race. The harness creates a fresh
// NewRootCmd per call but threads share package globals.
var externalMarkersEnabled atomic.Int32

func init() {
	externalMarkersEnabled.Store(1)
}

// setExternalMarkersDefault is called from renderEnvelope before
// dispatching to a prettyFunc.
func setExternalMarkersDefault(on bool) {
	if on {
		externalMarkersEnabled.Store(1)
	} else {
		externalMarkersEnabled.Store(0)
	}
}

// writeExternal writes a body of forge-supplied user content to w,
// wrapping it in `<<<EXTERNAL untrusted-content` / `EXTERNAL>>>`
// delimiters by default so an agent reading the output can refuse to
// follow instructions inside the marker. The --no-external-markers
// flag flips externalMarkersEnabled to 0; in that case the body is
// written verbatim with no wrapping, suitable for shell pipelines
// that want raw content.
//
// Empty bodies get no markers (and emit nothing) regardless of the
// flag — markers around empty content are noise.
//
// Mitigation: indirect prompt injection (#146).
func writeExternal(w io.Writer, body string) {
	if body == "" {
		return
	}
	if externalMarkersEnabled.Load() == 0 {
		_, _ = fmt.Fprint(w, body)
		return
	}
	// Trim trailing newlines so the closing marker sits on its own
	// line without a blank line before it.
	body = strings.TrimRight(body, "\n")
	_, _ = fmt.Fprintln(w, "<<<EXTERNAL untrusted-content")
	_, _ = fmt.Fprintln(w, body)
	_, _ = fmt.Fprintln(w, "EXTERNAL>>>")
}

// prettyFunc is the per-command human-readable renderer. nil means
// the command falls back to JSON regardless of --format.
type prettyFunc func(io.Writer, any) error

// renderEnvelope wraps data in the standard envelope, applies any
// flag-supplied projection, and writes either indented JSON or the
// command-supplied pretty rendering to cmd.OutOrStdout().
//
// page may be nil for non-list commands.
func renderEnvelope(cmd *cobra.Command, flags *globalFlags, data any, page *provider.Page, pretty prettyFunc) error {
	if flags.Format == "pretty" && pretty != nil {
		// Stash the operator's --no-external-markers preference for
		// the pretty rendering layer to consult via externalMarkers.
		// Pretty renderers wrap forge-supplied user content (issue
		// bodies, comments, wiki content, etc.) in <<<EXTERNAL /
		// EXTERNAL>>> delimiters by default — agents can branch on
		// the markers to refuse to follow instructions inside
		// untrusted text. The flag opts out for tooling that
		// processes the raw output. (#146)
		setExternalMarkersDefault(!flags.NoExternalMarkers)
		return pretty(cmd.OutOrStdout(), data)
	}

	env := envelope.New(data).WithPage(page)
	if flags.Fields != "" {
		if err := env.Project(flags.Fields); err != nil {
			return err
		}
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if err := enc.Encode(env); err != nil {
		return fmt.Errorf("encode envelope: %w", err)
	}
	return nil
}
