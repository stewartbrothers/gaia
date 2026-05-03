package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/envelope"
	"github.com/stewartbrothers/gaia/core/exitcode"
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
//
// --format ndjson on a single-resource (non-list) command is rejected
// with a usage error: streaming a single object is meaningless. List
// commands that want NDJSON go through renderListStreaming below
// instead, which never reaches this function on the streaming path.
func renderEnvelope(cmd *cobra.Command, flags *globalFlags, data any, page *provider.Page, pretty prettyFunc) error {
	if flags.Format == "ndjson" {
		return exitcode.Errorf(exitcode.Usage,
			"--format ndjson is only valid on list-style commands; use --format json for single-resource fetches")
	}
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

// PageFetcher returns one page of items at the given cursor. Empty
// cursor means "first page". The returned *provider.Page tells the
// caller whether to keep going (Truncated=true → call again with the
// returned NextCursor).
//
// Items are returned as []any so a single helper can serve every list
// command — the streaming path doesn't introspect the items, it just
// hands them to envelope.StreamWriter for marshaling.
type PageFetcher func(cursor string) (items []any, page *provider.Page, err error)

// renderListStreaming is the NDJSON code path for list-style commands.
// It loops over pages via fetch, emitting each item as one NDJSON line,
// then writes a `_metadata` trailer with the total count and the
// final next_cursor (if pagination was truncated).
//
// Cancellation: when the consumer closes its stdout pipe, the
// StreamWriter's next Write returns io.ErrClosedPipe; we exit the loop
// without invoking fetch again. That's the broken-pipe path agents use
// to bound their reads (`gaia issue list --format ndjson | head -1`).
//
// Falls through to renderEnvelope when --format is not ndjson — the
// helper is the single entry point a list command's RunE calls.
//
// The non-streaming path collects every page and renders one combined
// envelope, which preserves the existing JSON wire shape for
// --format json and --format pretty consumers. (We could short-circuit
// after the first page for parity with the old behaviour, but pagination
// loops only matter once a caller passes a cursor — and the existing
// list commands don't auto-page either, so a single fetch is what the
// non-streaming branch actually does.)
func renderListStreaming(cmd *cobra.Command, flags *globalFlags, fetch PageFetcher, pretty prettyFunc) error {
	if flags.Format != "ndjson" {
		// Non-streaming path: fetch one page (matches existing
		// per-command behaviour) and hand off to renderEnvelope.
		items, page, err := fetch(flags.Cursor)
		if err != nil {
			return err
		}
		return renderEnvelope(cmd, flags, items, page, pretty)
	}

	sw := envelope.NewStreamWriter(cmd.OutOrStdout())
	cursor := flags.Cursor
	total := 0
	var lastPage *provider.Page

	for {
		items, page, err := fetch(cursor)
		if err != nil {
			return err
		}
		lastPage = page
		for _, item := range items {
			if err := sw.Write(item); err != nil {
				if errors.Is(err, io.ErrClosedPipe) {
					// Consumer closed stdout — stop fetching, do
					// not write a trailer (the consumer doesn't
					// want it and the pipe write would just fail
					// again).
					return nil
				}
				return err
			}
			total++
		}
		// Continue paginating only if the caller did not pass an
		// explicit --cursor (which signals "give me one page from
		// here") and the page reports more results upstream.
		if flags.Cursor != "" {
			break
		}
		if page == nil || !page.Truncated || page.NextCursor == "" || page.NextCursor == cursor {
			break
		}
		cursor = page.NextCursor
	}

	nextCursor := ""
	if lastPage != nil && lastPage.Truncated && flags.Cursor != "" {
		// User asked for one page from a cursor; surface the next
		// cursor so they can resume.
		nextCursor = lastPage.NextCursor
	}
	trailer := envelope.NewMetadata(total, nextCursor)
	if err := sw.WriteTrailer(trailer); err != nil {
		if errors.Is(err, io.ErrClosedPipe) {
			return nil
		}
		return err
	}
	return nil
}

// renderListStreamingForTest is a cobra-free entry point used by the
// export_test shim so cli_test can drive the streaming helper directly.
// Constructs a minimal *cobra.Command with the supplied writer as
// stdout and the supplied flags so renderListStreaming runs end-to-end.
func renderListStreamingForTest(format, cursor string, fetch PageFetcher, w io.Writer) error {
	cmd := &cobra.Command{}
	cmd.SetOut(w)
	flags := &globalFlags{Format: format, Cursor: cursor}
	return renderListStreaming(cmd, flags, fetch, nil)
}

// renderEnvelopeRejectsNDJSONForTest exercises the single-resource
// rejection branch in renderEnvelope.
func renderEnvelopeRejectsNDJSONForTest(w io.Writer) error {
	cmd := &cobra.Command{}
	cmd.SetOut(w)
	flags := &globalFlags{Format: "ndjson"}
	return renderEnvelope(cmd, flags, map[string]any{"x": 1}, nil, nil)
}
