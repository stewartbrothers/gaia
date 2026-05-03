package envelope

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/stewartbrothers/gaia/core/types"
)

// StreamWriter emits a sequence of JSON objects, one per line, to an
// underlying io.Writer. The wire shape is "newline-delimited JSON"
// (NDJSON):
//
//	{"item": {...}}
//	{"item": {...}}
//	{"item": {...}}
//	{"_metadata": {"total": 3, "next_cursor": "...", "schema_version": "1.0"}}
//
// Each emitted line is independently valid JSON; a streaming consumer
// reads `bufio.Scanner` line-by-line and decodes each one without
// waiting for the whole list. The final trailer line is shape-tagged
// with `_metadata` so consumers can branch on the leading key without
// peeking at offsets.
//
// StreamWriter applies the same trust-tag rewriting as the regular
// Envelope marshaler — fields tagged `gaia:"trust=external"` emit
// `{"_trust":"external","_value":"<text>"}` on every streamed line, so
// indirect-prompt-injection mitigation (#146) carries over to NDJSON
// output.
//
// Buffering: StreamWriter wraps the underlying writer in a bufio.Writer
// internally and Flushes after every emitted line. That matters when
// the consumer is a pipe: without per-line flush, Go's default stdout
// buffering would batch lines and defeat the streaming purpose.
//
// Cancellation: when the consumer closes its end of a pipe, the next
// Write returns an io.ErrClosedPipe (or syscall.EPIPE on some
// platforms). StreamWriter surfaces that error verbatim so the caller's
// pagination loop can exit promptly. Callers that want context-aware
// cancellation should also pass a cancellable context into the
// provider's list call.
type StreamWriter struct {
	bw *bufio.Writer
}

// NewStreamWriter returns a StreamWriter that emits NDJSON to w.
// Callers are responsible for closing w themselves; StreamWriter does
// not own the underlying writer.
func NewStreamWriter(w io.Writer) *StreamWriter {
	return &StreamWriter{
		bw: bufio.NewWriter(w),
	}
}

// Write emits one line of NDJSON wrapping item under the "item" key,
// then flushes the underlying writer so the consumer sees the line
// immediately.
//
// Item-shape rewriting: trust-tagged fields are rewritten in the same
// way as the standard Envelope marshaler, so a list of issues with
// `gaia:"trust=external"` body fields emits per-line markers without
// the caller having to do anything special.
//
// Error wrapping: a write failure (broken pipe, full disk, ...) is
// returned verbatim so callers can branch on errors.Is(err,
// io.ErrClosedPipe) to detect a cancelled consumer.
func (s *StreamWriter) Write(item any) error {
	wrapped := struct {
		Item any `json:"item"`
	}{
		Item: applyTrustTags(item),
	}
	raw, err := json.Marshal(wrapped)
	if err != nil {
		return fmt.Errorf("ndjson: marshal item: %w", err)
	}
	return s.writeLine(raw)
}

// WriteTrailer emits the metadata-tagged final line. meta is the inner
// trailer object, NOT pre-wrapped in `{"_metadata": ...}` — Write
// adds the wrapper key so callers don't have to remember it.
//
// Use envelope.NewMetadata(total, nextCursor) to build a trailer with
// the canonical shape (schema_version stamped, optional next_cursor).
func (s *StreamWriter) WriteTrailer(meta any) error {
	wrapped := struct {
		Metadata any `json:"_metadata"`
	}{
		Metadata: applyTrustTags(meta),
	}
	raw, err := json.Marshal(wrapped)
	if err != nil {
		return fmt.Errorf("ndjson: marshal trailer: %w", err)
	}
	return s.writeLine(raw)
}

// writeLine is the shared "emit one line + newline + flush" path.
// Returns the underlying writer's error verbatim so callers can detect
// cancellation via errors.Is(io.ErrClosedPipe).
func (s *StreamWriter) writeLine(b []byte) error {
	if _, err := s.bw.Write(b); err != nil {
		return wrapPipeErr(err)
	}
	if err := s.bw.WriteByte('\n'); err != nil {
		return wrapPipeErr(err)
	}
	if err := s.bw.Flush(); err != nil {
		return wrapPipeErr(err)
	}
	return nil
}

// wrapPipeErr preserves errors.Is identity for io.ErrClosedPipe and
// io.EOF — both signal "consumer went away" — while attaching a
// human-readable prefix.
func wrapPipeErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, io.ErrClosedPipe):
		return fmt.Errorf("ndjson: %w", io.ErrClosedPipe)
	default:
		return fmt.Errorf("ndjson: write line: %w", err)
	}
}

// NewMetadata builds a trailer object with the canonical NDJSON shape:
//
//	{"total": <int>, "next_cursor": "<cursor>", "schema_version": "1.0"}
//
// next_cursor is omitted (set to nil) when the empty string is passed,
// matching the regular envelope's `_next_cursor` omit-empty behaviour.
// Callers can pass the result straight to StreamWriter.WriteTrailer.
func NewMetadata(total int, nextCursor string) map[string]any {
	out := map[string]any{
		"schema_version": types.SchemaVersion,
		"total":          total,
	}
	if nextCursor != "" {
		out["next_cursor"] = nextCursor
	} else {
		// Explicit null so consumers can branch on the key
		// existing-but-null vs the key being missing entirely.
		out["next_cursor"] = nil
	}
	return out
}
