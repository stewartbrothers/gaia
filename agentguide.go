// Package agentguide embeds docs/agent-guide.md into the binary so
// `gaia learn` (and the corresponding MCP resource) can serve the
// canonical agent-onboarding briefing without depending on a file
// next to the binary at runtime.
//
// The embed lives at the module root because //go:embed patterns
// cannot escape the directory tree of the file containing the
// directive — keeping the embed here lets docs/agent-guide.md remain
// the single source of truth, with no copy/symlink dance.
package agentguide

import _ "embed"

// Markdown is the verbatim contents of docs/agent-guide.md at build
// time. Do not mutate the returned string; consumers should treat it
// as read-only data.
//
// Wired up in cmd/gaia (via internal/cli/learn.go) and exposed
// through `gaia learn`. Kept byte-identical to docs/agent-guide.md by
// the unit test in agentguide_test.go — any drift fails CI.
//
//go:embed docs/agent-guide.md
var Markdown string
