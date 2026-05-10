# `gaia gitignore` measurement

`gaia gitignore` is a static, self-contained command — no forge call,
no curl baseline. The measurement that matters is how big the
recommended block is, both as raw text (default output) and wrapped
in the standard envelope (`--format json`).

| Output mode | Bytes | Notes |
|---|---|---|
| `gaia gitignore` (raw text) | 281 | Verbatim contents of `internal/gitignore/recommended.txt`, suitable for `>> .gitignore`. |
| `gaia gitignore --format json` | 220 | Standard envelope, `data.entries: [...]`. Strips comments and blank lines from the embedded block; the JSON shape is what an MCP-style consumer reads. |
| `gaia gitignore --check` (happy) | 45 | Single confirmation line: ".gitignore covers every recommended entry." |
| `gaia gitignore --check` (4 missing) | 124 | Banner + one line per missing entry. |

The point of the comparison isn't a curl baseline — there is no
forge-side equivalent, the recommended block lives in the binary.
The point is that `gaia gitignore --format json` is a stable
envelope-shaped payload an agent can decode with the same parser it
uses for every other gaia output. No bespoke "is this a list of
strings" branch needed.

## CI gating

```bash
gaia gitignore --check --quiet || {
  echo ".gitignore is out of date — run 'gaia gitignore >> .gitignore'"
  exit 1
}
```

Exit code is `0` when the project's `.gitignore` covers every
recommended entry, `1` (Generic) when entries are missing. Pinning
this in CI catches a project drifting out of sync with the
recommendations the moment it lands.

## MCP resource

The same content is exposed as a static MCP resource at
`gaia://gitignore` (MIME `text/plain`). Agents driving `gaia-mcp`
can `resources/read` the URI; the bytes returned are byte-identical
to `gaia gitignore`'s default output.
