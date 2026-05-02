package cli

// NormalizeForgejoURLForTest exposes the internal normalizeForgejoURL
// helper to the cli_test package. Test-only re-export.
func NormalizeForgejoURLForTest(raw string) string {
	return normalizeForgejoURL(raw)
}
