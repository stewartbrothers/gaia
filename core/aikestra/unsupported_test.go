package aikestra

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

// Every method of a port Aikestra declares unsupported answers
// NotImplemented without touching the network.
func TestUnsupportedPortsAreNotImplemented(t *testing.T) {
	ports := []any{
		(*provider.PullRequestOps)(nil), (*provider.SearchOps)(nil), (*provider.LabelOps)(nil),
		(*provider.ReleaseOps)(nil), (*provider.PackageOps)(nil), (*provider.WikiOps)(nil),
		(*provider.WebhookOps)(nil), (*provider.ActionsOps)(nil), (*provider.MilestoneOps)(nil),
		(*provider.BranchProtectionOps)(nil), (*provider.SecretsOps)(nil), (*provider.VariablesOps)(nil),
		(*provider.RunnersOps)(nil), (*provider.CollaboratorsOps)(nil),
	}
	p := reflect.ValueOf(replay(t))
	for _, port := range ports {
		it := reflect.TypeOf(port).Elem()
		for i := 0; i < it.NumMethod(); i++ {
			name := it.Method(i).Name
			m := p.MethodByName(name)
			args := make([]reflect.Value, m.Type().NumIn())
			for j := range args {
				args[j] = reflect.Zero(m.Type().In(j))
			}
			args[0] = reflect.ValueOf(context.Background())
			out := m.Call(args)
			err, _ := out[len(out)-1].Interface().(error)
			if exitcode.Of(err) != exitcode.NotImplemented || !strings.Contains(err.Error(), "aikestra") {
				t.Errorf("%s.%s: %v", it.Name(), name, err)
			}
		}
	}
}

func TestScrubError(t *testing.T) {
	err := scrubError(errors.New("dial https://x?t=secret-token: refused"), "secret-token")
	if strings.Contains(err.Error(), "secret-token") || !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("not scrubbed: %v", err)
	}
	plain := errors.New("refused")
	if scrubError(plain, "secret-token") != plain || scrubError(nil, "x") != nil {
		t.Fatal("scrubError changed an error without the token")
	}
}

func TestDeleteIssueCommentNotImplemented(t *testing.T) {
	wantCode(t, replay(t).DeleteIssueComment(context.Background(), "acme", "", 1), exitcode.NotImplemented, "")
}
