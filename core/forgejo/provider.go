package forgejo

// Provider implements core/provider.Provider for Forgejo (and
// Gitea-compatible) forges. Methods are added in phases — this PR
// (#15) lands ListIssues + GetIssue; remaining methods follow in
// #16..#19. The interface assertion in core/provider/provider_test.go
// is currently exercised against a noopProvider so partial
// implementations don't break the build.
type Provider struct {
	client *Client
}

// NewProvider builds a Provider over a freshly-constructed Client.
// Callers that already hold a *Client (e.g. for non-Provider calls
// like fetching the Forgejo version) can construct
// `&Provider{client: c}` directly via the package-internal field —
// keep that internal so the public surface is the convenience
// constructor.
func NewProvider(opts Options) *Provider {
	return &Provider{client: New(opts)}
}
