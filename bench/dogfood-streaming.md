# NDJSON streaming baseline

Measured during PR #151 against a 100-issue stub fake-forge fixture.

| Operation                                    | `--format json` | `--format ndjson` |
|----------------------------------------------|-----------------|-------------------|
| Bytes to first usable item (100 issues)      | 53 KB           | 359 B (~150× faster) |
| Total bytes (100 issues)                     | 53 KB           | 36 KB + 70 B trailer |
| Pages fetched on `\| head -1` (broken pipe)  | n/a             | exactly 1 (cancellation works) |

Streaming-enabled commands: `gaia issue list`, `gaia pr list`,
`gaia pr comments`, `gaia label list`, `gaia release list`,
`gaia wiki list`, `gaia packages list`, `gaia webhook list`,
`gaia webhook deliveries`, `gaia search`.

Per-line trust marker preservation: every emitted line carries the
same `_trust=external` tag as `--format json` would for the same
field (issue body, comment body, etc.). `--no-external-markers`
opts out of the pretty wrapping but JSON `_trust` tags persist.

Re-run with `make dogfood-stream` once the harness gains a
`--format ndjson` toggle.
