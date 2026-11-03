package aikestra

// Every method here covers a resource Aikestra API v1 doesn't have
// (#404). The registration declares the matching capabilities
// unsupported, so the CLI and MCP surfaces hide them; direct callers get
// exitcode.NotImplemented.

import (
	"context"
	"io"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// ListPullRequests is not in Aikestra API v1.
func (p *Provider) ListPullRequests(_ context.Context, _ string, _ string, _ provider.ListPullRequestsOptions) ([]types.PullRequest, *provider.Page, error) {
	return nil, nil, notImplemented("pull requests")
}

// GetPullRequest is not in Aikestra API v1.
func (p *Provider) GetPullRequest(_ context.Context, _ string, _ string, _ int, _ provider.GetPullRequestOptions) (*types.PullRequest, error) {
	return nil, notImplemented("pull requests")
}

// GetPullRequestDiff is not in Aikestra API v1.
func (p *Provider) GetPullRequestDiff(_ context.Context, _ string, _ string, _ int, _ provider.GetPullRequestDiffOptions) ([]types.DiffFile, error) {
	return nil, notImplemented("pull requests")
}

// GetCommitStatus is not in Aikestra API v1.
func (p *Provider) GetCommitStatus(_ context.Context, _ string, _ string, _ string) (*types.CISummary, error) {
	return nil, notImplemented("pull requests")
}

// CreatePullRequest is not in Aikestra API v1.
func (p *Provider) CreatePullRequest(_ context.Context, _ string, _ string, _ provider.CreatePullRequestOptions) (*types.PullRequest, error) {
	return nil, notImplemented("pull requests")
}

// EditPullRequest is not in Aikestra API v1.
func (p *Provider) EditPullRequest(_ context.Context, _ string, _ string, _ int, _ provider.EditPullRequestOptions) (*types.PullRequest, error) {
	return nil, notImplemented("pull requests")
}

// MergePullRequest is not in Aikestra API v1.
func (p *Provider) MergePullRequest(_ context.Context, _ string, _ string, _ int, _ provider.MergePullRequestOptions) error {
	return notImplemented("pull requests")
}

// SubmitReview is not in Aikestra API v1.
func (p *Provider) SubmitReview(_ context.Context, _ string, _ string, _ int, _ provider.SubmitReviewOptions) error {
	return notImplemented("pull requests")
}

// ListLabels is not in Aikestra API v1.
func (p *Provider) ListLabels(_ context.Context, _ string, _ string, _ provider.ListLabelsOptions) ([]types.Label, error) {
	return nil, notImplemented("labels")
}

// CreateLabel is not in Aikestra API v1.
func (p *Provider) CreateLabel(_ context.Context, _ string, _ string, _ provider.CreateLabelOptions) (*types.Label, error) {
	return nil, notImplemented("labels")
}

// EditLabel is not in Aikestra API v1.
func (p *Provider) EditLabel(_ context.Context, _ string, _ string, _ string, _ provider.EditLabelOptions) (*types.Label, error) {
	return nil, notImplemented("labels")
}

// DeleteLabel is not in Aikestra API v1.
func (p *Provider) DeleteLabel(_ context.Context, _ string, _ string, _ string) error {
	return notImplemented("labels")
}

// ListReleases is not in Aikestra API v1.
func (p *Provider) ListReleases(_ context.Context, _ string, _ string, _ provider.ListReleasesOptions) ([]types.Release, *provider.Page, error) {
	return nil, nil, notImplemented("releases")
}

// GetRelease is not in Aikestra API v1.
func (p *Provider) GetRelease(_ context.Context, _ string, _ string, _ string) (*types.Release, error) {
	return nil, notImplemented("releases")
}

// CreateRelease is not in Aikestra API v1.
func (p *Provider) CreateRelease(_ context.Context, _ string, _ string, _ provider.CreateReleaseOptions) (*types.Release, error) {
	return nil, notImplemented("releases")
}

// EditRelease is not in Aikestra API v1.
func (p *Provider) EditRelease(_ context.Context, _ string, _ string, _ string, _ provider.EditReleaseOptions) (*types.Release, error) {
	return nil, notImplemented("releases")
}

// DeleteRelease is not in Aikestra API v1.
func (p *Provider) DeleteRelease(_ context.Context, _ string, _ string, _ string) error {
	return notImplemented("releases")
}

// ListReleaseAssets is not in Aikestra API v1.
func (p *Provider) ListReleaseAssets(_ context.Context, _ string, _ string, _ int64) ([]types.ReleaseAsset, error) {
	return nil, notImplemented("releases")
}

// UploadReleaseAsset is not in Aikestra API v1.
func (p *Provider) UploadReleaseAsset(_ context.Context, _ string, _ string, _ int64, _ string, _ string, _ int64, _ io.Reader) error {
	return notImplemented("releases")
}

// DeleteReleaseAsset is not in Aikestra API v1.
func (p *Provider) DeleteReleaseAsset(_ context.Context, _ string, _ string, _ int64, _ int64) error {
	return notImplemented("releases")
}

// ListPackages is not in Aikestra API v1.
func (p *Provider) ListPackages(_ context.Context, _ string, _ provider.ListPackagesOptions) ([]types.Package, *provider.Page, error) {
	return nil, nil, notImplemented("packages")
}

// GetPackage is not in Aikestra API v1.
func (p *Provider) GetPackage(_ context.Context, _ string, _ string, _ string, _ string) (*types.Package, error) {
	return nil, notImplemented("packages")
}

// DeletePackage is not in Aikestra API v1.
func (p *Provider) DeletePackage(_ context.Context, _ string, _ string, _ string, _ string) error {
	return notImplemented("packages")
}

// UploadPackage is not in Aikestra API v1.
func (p *Provider) UploadPackage(_ context.Context, _ string, _ string, _ string, _ string, _ provider.UploadPackageOptions, _ io.Reader) error {
	return notImplemented("packages")
}

// ListWikiPages is not in Aikestra API v1.
func (p *Provider) ListWikiPages(_ context.Context, _ string, _ string, _ provider.ListWikiPagesOptions) ([]types.WikiPage, *provider.Page, error) {
	return nil, nil, notImplemented("wikis")
}

// GetWikiPage is not in Aikestra API v1.
func (p *Provider) GetWikiPage(_ context.Context, _ string, _ string, _ string) (*types.WikiPage, error) {
	return nil, notImplemented("wikis")
}

// SearchWikiPages is not in Aikestra API v1.
func (p *Provider) SearchWikiPages(_ context.Context, _ string, _ string, _ string, _ provider.SearchWikiOptions) ([]types.WikiSearchHit, error) {
	return nil, notImplemented("wikis")
}

// EditWikiPage is not in Aikestra API v1.
func (p *Provider) EditWikiPage(_ context.Context, _ string, _ string, _ string, _ string) (*types.WikiPage, error) {
	return nil, notImplemented("wikis")
}

// DeleteWikiPage is not in Aikestra API v1.
func (p *Provider) DeleteWikiPage(_ context.Context, _ string, _ string, _ string) error {
	return notImplemented("wikis")
}

// ListWebhooks is not in Aikestra API v1.
func (p *Provider) ListWebhooks(_ context.Context, _ string, _ string, _ provider.ListWebhooksOptions) ([]types.Webhook, *provider.Page, error) {
	return nil, nil, notImplemented("webhooks")
}

// GetWebhook is not in Aikestra API v1.
func (p *Provider) GetWebhook(_ context.Context, _ string, _ string, _ int64) (*types.Webhook, error) {
	return nil, notImplemented("webhooks")
}

// CreateWebhook is not in Aikestra API v1.
func (p *Provider) CreateWebhook(_ context.Context, _ string, _ string, _ provider.CreateWebhookOptions) (*types.Webhook, error) {
	return nil, notImplemented("webhooks")
}

// EditWebhook is not in Aikestra API v1.
func (p *Provider) EditWebhook(_ context.Context, _ string, _ string, _ int64, _ provider.EditWebhookOptions) (*types.Webhook, error) {
	return nil, notImplemented("webhooks")
}

// DeleteWebhook is not in Aikestra API v1.
func (p *Provider) DeleteWebhook(_ context.Context, _ string, _ string, _ int64) error {
	return notImplemented("webhooks")
}

// ListWebhookDeliveries is not in Aikestra API v1.
func (p *Provider) ListWebhookDeliveries(_ context.Context, _ string, _ string, _ int64, _ provider.ListDeliveriesOptions) ([]types.WebhookDelivery, *provider.Page, error) {
	return nil, nil, notImplemented("webhooks")
}

// GetWebhookDelivery is not in Aikestra API v1.
func (p *Provider) GetWebhookDelivery(_ context.Context, _ string, _ string, _ int64, _ int64) (*types.WebhookDeliveryDetail, error) {
	return nil, notImplemented("webhooks")
}

// RedeliverWebhook is not in Aikestra API v1.
func (p *Provider) RedeliverWebhook(_ context.Context, _ string, _ string, _ int64, _ int64) error {
	return notImplemented("webhooks")
}

// TestWebhook is not in Aikestra API v1.
func (p *Provider) TestWebhook(_ context.Context, _ string, _ string, _ int64) error {
	return notImplemented("webhooks")
}

// ListWorkflowRuns is not in Aikestra API v1.
func (p *Provider) ListWorkflowRuns(_ context.Context, _ string, _ string, _ provider.ListWorkflowRunsOptions) ([]types.WorkflowRun, *provider.Page, error) {
	return nil, nil, notImplemented("Actions")
}

// GetWorkflowRun is not in Aikestra API v1.
func (p *Provider) GetWorkflowRun(_ context.Context, _ string, _ string, _ int64, _ provider.GetWorkflowRunOptions) (*types.WorkflowRun, error) {
	return nil, notImplemented("Actions")
}

// GetWorkflowRunLogs is not in Aikestra API v1.
func (p *Provider) GetWorkflowRunLogs(_ context.Context, _ string, _ string, _ int64, _ provider.GetWorkflowRunLogsOptions) ([]types.WorkflowRunLogs, error) {
	return nil, notImplemented("Actions")
}

// RerunWorkflowRun is not in Aikestra API v1.
func (p *Provider) RerunWorkflowRun(_ context.Context, _ string, _ string, _ int64) error {
	return notImplemented("Actions")
}

// ListMilestones is not in Aikestra API v1.
func (p *Provider) ListMilestones(_ context.Context, _ string, _ string, _ provider.ListMilestonesOptions) ([]types.Milestone, *provider.Page, error) {
	return nil, nil, notImplemented("milestones")
}

// GetMilestone is not in Aikestra API v1.
func (p *Provider) GetMilestone(_ context.Context, _ string, _ string, _ int64) (*types.Milestone, error) {
	return nil, notImplemented("milestones")
}

// CreateMilestone is not in Aikestra API v1.
func (p *Provider) CreateMilestone(_ context.Context, _ string, _ string, _ provider.CreateMilestoneOptions) (*types.Milestone, error) {
	return nil, notImplemented("milestones")
}

// EditMilestone is not in Aikestra API v1.
func (p *Provider) EditMilestone(_ context.Context, _ string, _ string, _ int64, _ provider.EditMilestoneOptions) (*types.Milestone, error) {
	return nil, notImplemented("milestones")
}

// DeleteMilestone is not in Aikestra API v1.
func (p *Provider) DeleteMilestone(_ context.Context, _ string, _ string, _ int64) error {
	return notImplemented("milestones")
}

// ListMilestoneIssues is not in Aikestra API v1.
func (p *Provider) ListMilestoneIssues(_ context.Context, _ string, _ string, _ int64, _ provider.ListMilestoneIssuesOptions) ([]types.Issue, *provider.Page, error) {
	return nil, nil, notImplemented("milestones")
}

// GetBranchProtection is not in Aikestra API v1.
func (p *Provider) GetBranchProtection(_ context.Context, _ string, _ string, _ string) (*types.BranchProtection, error) {
	return nil, notImplemented("branch protection")
}

// SetBranchProtection is not in Aikestra API v1.
func (p *Provider) SetBranchProtection(_ context.Context, _ string, _ string, _ string, _ provider.SetBranchProtectionOptions) (*types.BranchProtection, error) {
	return nil, notImplemented("branch protection")
}

// DeleteBranchProtection is not in Aikestra API v1.
func (p *Provider) DeleteBranchProtection(_ context.Context, _ string, _ string, _ string) error {
	return notImplemented("branch protection")
}

// ListSecrets is not in Aikestra API v1.
func (p *Provider) ListSecrets(_ context.Context, _ string, _ string, _ provider.ListSecretsOptions) ([]types.Secret, *provider.Page, error) {
	return nil, nil, notImplemented("secrets")
}

// ListVariables is not in Aikestra API v1.
func (p *Provider) ListVariables(_ context.Context, _ string, _ string, _ provider.ListVariablesOptions) ([]types.Variable, *provider.Page, error) {
	return nil, nil, notImplemented("variables")
}

// ListRunners is not in Aikestra API v1.
func (p *Provider) ListRunners(_ context.Context, _ string, _ string, _ provider.ListRunnersOptions) ([]types.Runner, *provider.Page, error) {
	return nil, nil, notImplemented("runners")
}

// ListCollaborators is not in Aikestra API v1.
func (p *Provider) ListCollaborators(_ context.Context, _ string, _ string, _ provider.ListCollaboratorsOptions) ([]types.Collaborator, *provider.Page, error) {
	return nil, nil, notImplemented("collaborators")
}
