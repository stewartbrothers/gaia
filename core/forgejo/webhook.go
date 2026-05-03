package forgejo

import (
	"context"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// Webhook methods are stubbed in commit 1 (#85 types + interface)
// and filled in by commits 2 and 3 of the same stack. Returning a
// generic exitcode error keeps the Provider interface satisfied so
// the rest of the build stays green.

// ListWebhooks is implemented in a follow-up commit.
func (p *Provider) ListWebhooks(_ context.Context, _, _ string, _ provider.ListWebhooksOptions) ([]types.Webhook, *provider.Page, error) {
	return nil, nil, exitcode.Errorf(exitcode.Generic, "forgejo: ListWebhooks not yet implemented")
}

// GetWebhook is implemented in a follow-up commit.
func (p *Provider) GetWebhook(_ context.Context, _, _ string, _ int64) (*types.Webhook, error) {
	return nil, exitcode.Errorf(exitcode.Generic, "forgejo: GetWebhook not yet implemented")
}

// CreateWebhook is implemented in a follow-up commit.
func (p *Provider) CreateWebhook(_ context.Context, _, _ string, _ provider.CreateWebhookOptions) (*types.Webhook, error) {
	return nil, exitcode.Errorf(exitcode.Generic, "forgejo: CreateWebhook not yet implemented")
}

// EditWebhook is implemented in a follow-up commit.
func (p *Provider) EditWebhook(_ context.Context, _, _ string, _ int64, _ provider.EditWebhookOptions) (*types.Webhook, error) {
	return nil, exitcode.Errorf(exitcode.Generic, "forgejo: EditWebhook not yet implemented")
}

// DeleteWebhook is implemented in a follow-up commit.
func (p *Provider) DeleteWebhook(_ context.Context, _, _ string, _ int64) error {
	return exitcode.Errorf(exitcode.Generic, "forgejo: DeleteWebhook not yet implemented")
}

// ListWebhookDeliveries is implemented in a follow-up commit.
func (p *Provider) ListWebhookDeliveries(_ context.Context, _, _ string, _ int64, _ provider.ListDeliveriesOptions) ([]types.WebhookDelivery, *provider.Page, error) {
	return nil, nil, exitcode.Errorf(exitcode.Generic, "forgejo: ListWebhookDeliveries not yet implemented")
}

// GetWebhookDelivery is implemented in a follow-up commit.
func (p *Provider) GetWebhookDelivery(_ context.Context, _, _ string, _, _ int64) (*types.WebhookDeliveryDetail, error) {
	return nil, exitcode.Errorf(exitcode.Generic, "forgejo: GetWebhookDelivery not yet implemented")
}

// RedeliverWebhook is implemented in a follow-up commit.
func (p *Provider) RedeliverWebhook(_ context.Context, _, _ string, _, _ int64) error {
	return exitcode.Errorf(exitcode.Generic, "forgejo: RedeliverWebhook not yet implemented")
}

// TestWebhook is implemented in a follow-up commit.
func (p *Provider) TestWebhook(_ context.Context, _, _ string, _ int64) error {
	return exitcode.Errorf(exitcode.Generic, "forgejo: TestWebhook not yet implemented")
}
