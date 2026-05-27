// Package github: Actions stubs (#183).
//
// GitHub Actions is a Phase 2 item. All four methods return a documented
// "not implemented" error so callers get a clear message rather than a
// confusing 404 or panic. Implement as part of the Phase 2 GitHub provider
// parity work.
package github

import (
	"context"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// ListWorkflowRuns is not yet implemented for GitHub. TODO: Phase 2.
func (p *Provider) ListWorkflowRuns(_ context.Context, _, _ string, _ provider.ListWorkflowRunsOptions) ([]types.WorkflowRun, *provider.Page, error) {
	return nil, nil, exitcode.Errorf(exitcode.NotImplemented,
		"GitHub Actions is not yet implemented — tracked in Phase 2 (#183)")
}

// GetWorkflowRun is not yet implemented for GitHub. TODO: Phase 2.
func (p *Provider) GetWorkflowRun(_ context.Context, _, _ string, _ int64, _ provider.GetWorkflowRunOptions) (*types.WorkflowRun, error) {
	return nil, exitcode.Errorf(exitcode.NotImplemented,
		"GitHub Actions is not yet implemented — tracked in Phase 2 (#183)")
}

// GetWorkflowRunLogs is not yet implemented for GitHub. TODO: Phase 2.
func (p *Provider) GetWorkflowRunLogs(_ context.Context, _, _ string, _ int64, _ provider.GetWorkflowRunLogsOptions) ([]types.WorkflowRunLogs, error) {
	return nil, exitcode.Errorf(exitcode.NotImplemented,
		"GitHub Actions is not yet implemented — tracked in Phase 2 (#183)")
}

// RerunWorkflowRun is not yet implemented for GitHub. TODO: Phase 2.
func (p *Provider) RerunWorkflowRun(_ context.Context, _, _ string, _ int64) error {
	return exitcode.Errorf(exitcode.NotImplemented,
		"GitHub Actions is not yet implemented — tracked in Phase 2 (#183)")
}
