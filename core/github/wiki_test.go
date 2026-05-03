package github_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

// GitHub wikis aren't a REST resource — they live as a separate git
// repo at {owner}/{repo}.wiki.git. Until the clone-cache lands (#120),
// every wiki method returns a clear NotImplemented error pointing at
// the tracking issue so the caller fails fast.

func TestGitHubWikiListReturnsNotImplemented(t *testing.T) {
	p := newTestProvider(t, "https://api.example")
	_, _, err := p.ListWikiPages(context.Background(), "o", "r", provider.ListWikiPagesOptions{})
	if err == nil {
		t.Fatal("expected NotImplemented error")
	}
	if !strings.Contains(err.Error(), "#120") {
		t.Errorf("error should reference tracking issue #120; got %q", err.Error())
	}
	if got := exitcode.Of(err); got != exitcode.Generic {
		t.Errorf("got exit code %d, want Generic", got)
	}
}

func TestGitHubWikiGetReturnsNotImplemented(t *testing.T) {
	p := newTestProvider(t, "https://api.example")
	_, err := p.GetWikiPage(context.Background(), "o", "r", "Home")
	if err == nil || !strings.Contains(err.Error(), "#120") {
		t.Errorf("expected #120 reference; got %v", err)
	}
}

func TestGitHubWikiSearchReturnsNotImplemented(t *testing.T) {
	p := newTestProvider(t, "https://api.example")
	_, err := p.SearchWikiPages(context.Background(), "o", "r", "q", provider.SearchWikiOptions{})
	if err == nil || !strings.Contains(err.Error(), "#120") {
		t.Errorf("expected #120 reference; got %v", err)
	}
}

func TestGitHubWikiEditReturnsNotImplemented(t *testing.T) {
	p := newTestProvider(t, "https://api.example")
	_, err := p.EditWikiPage(context.Background(), "o", "r", "Home", "body")
	if err == nil || !strings.Contains(err.Error(), "#120") {
		t.Errorf("expected #120 reference; got %v", err)
	}
}

func TestGitHubWikiDeleteReturnsNotImplemented(t *testing.T) {
	p := newTestProvider(t, "https://api.example")
	err := p.DeleteWikiPage(context.Background(), "o", "r", "Home")
	if err == nil || !strings.Contains(err.Error(), "#120") {
		t.Errorf("expected #120 reference; got %v", err)
	}
}
