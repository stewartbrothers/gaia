package provider_test

import (
	"context"
	"testing"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// labelOnly implements ONLY provider.LabelOps. The point of this test is
// that it compiles and runs without stubbing the other ~45 Provider
// methods — the whole reason the narrow ports exist (ADR 0001 / #312).
// If LabelOps ever stopped being a standalone interface (e.g. folded
// back into the wide Provider), this file would fail to compile.
type labelOnly struct {
	listCalls int
}

func (l *labelOnly) ListLabels(_ context.Context, _, _ string, _ provider.ListLabelsOptions) ([]types.Label, error) {
	l.listCalls++
	return []types.Label{{Name: "bug"}}, nil
}
func (l *labelOnly) CreateLabel(_ context.Context, _, _ string, _ provider.CreateLabelOptions) (*types.Label, error) {
	return &types.Label{Name: "feat"}, nil
}
func (l *labelOnly) EditLabel(_ context.Context, _, _ string, _ string, _ provider.EditLabelOptions) (*types.Label, error) {
	return &types.Label{Name: "feat"}, nil
}
func (l *labelOnly) DeleteLabel(_ context.Context, _, _ string, _ string) error { return nil }

// countLabels is a consumer typed to the narrow port — exactly the shape
// a CLI handler or chain step takes when it only needs labels.
func countLabels(ctx context.Context, ops provider.LabelOps) (int, error) {
	labels, err := ops.ListLabels(ctx, "o", "r", provider.ListLabelsOptions{})
	return len(labels), err
}

func TestNarrowPortSubstitution(t *testing.T) {
	// Compile-time: a type implementing only LabelOps satisfies LabelOps.
	var ops provider.LabelOps = &labelOnly{}

	got, err := countLabels(context.Background(), ops)
	if err != nil {
		t.Fatalf("countLabels: %v", err)
	}
	if got != 1 {
		t.Fatalf("countLabels: got %d, want 1", got)
	}
}

// TestProviderComposesLabelOps pins that the wide Provider still
// satisfies the narrow port — any Provider can be passed where a
// LabelOps is wanted, so existing call sites keep working.
func TestProviderComposesLabelOps(t *testing.T) {
	var _ provider.LabelOps = (provider.Provider)(nil)
	var _ provider.ReleaseOps = (provider.Provider)(nil)
	var _ provider.WebhookOps = (provider.Provider)(nil)
}
