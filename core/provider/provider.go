// Package provider defines the contract every gaia backend implements.
// `cmd/gaia` and `cmd/gaia-mcp` both depend only on this interface and on
// `core/types`; per-forge code (Forgejo, GitHub) lives behind it as
// implementations chosen at runtime by config or git-remote auto-detect.
//
// Methods take owner+repo as parameters rather than baking them into the
// Provider value, so a single Provider can serve cross-repo flows
// (notably Search) without holding repo state.
package provider

// Provider is the unified API surface the CLI and MCP server both call.
// Implementations are responsible for translating their forge's REST
// shape into the trimmed core/types values, and for reconciling
// multi-endpoint reads (PR + checks, three comment endpoints, etc.) into
// a single return.
//
// Provider is a composition of the per-resource ports declared in
// ports.go. Code that needs everything (forgebuilder.Build, the chain
// orchestrator, the MCP tool dispatcher) keeps depending on Provider;
// code that needs one resource depends on the narrow port (e.g.
// [LabelOps], [ReleaseOps], [WebhookOps]) instead. Concrete forge
// implementations satisfy Provider by implementing every method, so the
// split is purely consumer-facing.
type Provider interface {
	IdentityOps
	IssueOps
	IssueDependencyOps
	BranchOps
	TagOps
	BranchProtectionOps
	CommentOps
	PullRequestOps
	SearchOps
	LabelOps
	ReleaseOps
	PackageOps
	WikiOps
	WebhookOps
	ActionsOps
	MilestoneOps
	SecretsOps
	VariablesOps
	RunnersOps
	CollaboratorsOps
}
