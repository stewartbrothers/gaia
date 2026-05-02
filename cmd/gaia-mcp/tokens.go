package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/stewartbrothers/gaia/core/exitcode"
)

// tokenStore maps a bearer token (the secret) to its label (a
// human-readable identifier used in audit logs). Labels are how
// operators tell which client called a tool when reading server
// stderr — never log the token itself.
type tokenStore map[string]string

// loadTokensFromFile parses a one-token-per-line file:
//
//	# comments allowed (#-prefix, full-line)
//	tok_abc123                # bare token, label auto-generated as token-N
//	tok_def456 alice           # token + space + free-form label
//	tok_ghi789 bob's-laptop    # multi-word labels are fine
//
// Mode must be 0600 or stricter (no group / other bits). A
// 0644-readable token file is a vulnerability — anyone with read
// access on the host can impersonate every configured client. We
// refuse to start instead of silently warning, same posture as ssh
// for ~/.ssh/id_rsa.
func loadTokensFromFile(path string) (tokenStore, error) {
	if path == "" {
		return nil, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, exitcode.Wrap(err, exitcode.Usage, "stat token file")
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return nil, exitcode.Errorf(exitcode.Usage,
			"token file %q is too permissive (mode %o); chmod 0600 to restrict to owner",
			path, mode)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, exitcode.Wrap(err, exitcode.Usage, "open token file")
	}
	defer func() { _ = f.Close() }()

	store := tokenStore{}
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		token, label := splitTokenLine(raw, lineNum)
		if existing, dup := store[token]; dup {
			return nil, exitcode.Errorf(exitcode.Usage,
				"token file %q line %d: duplicate token (also seen as label %q)",
				path, lineNum, existing)
		}
		store[token] = label
	}
	if err := scanner.Err(); err != nil {
		return nil, exitcode.Wrap(err, exitcode.Usage, "read token file")
	}

	if len(store) == 0 {
		return nil, exitcode.Errorf(exitcode.Usage,
			"token file %q has no tokens (only blank lines or comments)", path)
	}
	return store, nil
}

func splitTokenLine(line string, lineNum int) (token, label string) {
	idx := strings.IndexAny(line, " \t")
	if idx < 0 {
		return line, fmt.Sprintf("token-%d", lineNum)
	}
	token = strings.TrimSpace(line[:idx])
	label = strings.TrimSpace(line[idx+1:])
	if label == "" {
		label = fmt.Sprintf("token-%d", lineNum)
	}
	return token, label
}
