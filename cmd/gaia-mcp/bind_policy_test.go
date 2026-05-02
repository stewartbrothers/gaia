package main

import (
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
)

func TestIsLoopbackBind(t *testing.T) {
	cases := []struct {
		addr    string
		want    bool
		wantErr bool
	}{
		{"127.0.0.1:8080", true, false},
		{"127.0.0.1:0", true, false},
		{"[::1]:8080", true, false},
		{"localhost:8080", true, false},
		{"LOCALHOST:8080", true, false}, // case-insensitive
		{":8080", false, false},         // all interfaces — NOT loopback
		{"0.0.0.0:8080", false, false},
		{"10.0.0.1:8080", false, false},
		{"example.com:8080", false, false}, // hostname → conservative non-loopback
		{"not-an-addr", false, true},
	}
	for _, tc := range cases {
		got, err := isLoopbackBind(tc.addr)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%q: expected error, got nil", tc.addr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tc.addr, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestBindPolicyValidate(t *testing.T) {
	cases := []struct {
		name      string
		policy    bindPolicy
		wantOK    bool
		wantInMsg string
	}{
		{
			name:   "loopback → ok",
			policy: bindPolicy{Addr: "127.0.0.1:8080"},
			wantOK: true,
		},
		{
			name:   "loopback IPv6 → ok",
			policy: bindPolicy{Addr: "[::1]:8080"},
			wantOK: true,
		},
		{
			name:   "public + tls-acked → ok",
			policy: bindPolicy{Addr: ":8080", AllowPublicNoTLS: true},
			wantOK: true,
		},
		{
			name:      "public default → refused",
			policy:    bindPolicy{Addr: ":8080"},
			wantOK:    false,
			wantInMsg: "non-loopback",
		},
		{
			name:      "0.0.0.0 default → refused",
			policy:    bindPolicy{Addr: "0.0.0.0:8080"},
			wantOK:    false,
			wantInMsg: "non-loopback",
		},
		{
			name:      "public IP default → refused",
			policy:    bindPolicy{Addr: "10.0.0.1:8080"},
			wantOK:    false,
			wantInMsg: "non-loopback",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.validate()
			if tc.wantOK {
				if err != nil {
					t.Errorf("expected ok, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if exitcode.Of(err) != exitcode.Usage {
				t.Errorf("expected exitcode.Usage, got %d (%v)", exitcode.Of(err), err)
			}
			if tc.wantInMsg != "" && !strings.Contains(err.Error(), tc.wantInMsg) {
				t.Errorf("error message %q missing %q", err.Error(), tc.wantInMsg)
			}
		})
	}
}

// TestPolicyMessageMentionsTLS pins that the refusal text actually
// names TLS as the missing piece — operators reading the error need
// to know what the fix is. If a future refactor changes the wording
// to drop "TLS", this catches it.
func TestPolicyMessageMentionsTLS(t *testing.T) {
	err := bindPolicy{Addr: ":8080"}.validate()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "TLS") {
		t.Errorf("error must reference TLS as the missing piece; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "--allow-public-no-tls") {
		t.Errorf("error must reference the opt-out flag; got %q", err.Error())
	}
}

func TestRunRefusesPublicNoTLS(t *testing.T) {
	err := run([]string{"--http", "0.0.0.0:0"})
	if err == nil {
		t.Fatal("expected refusal for non-loopback no-TLS")
	}
	if !strings.Contains(err.Error(), "non-loopback") {
		t.Errorf("expected 'non-loopback' in error; got %q", err.Error())
	}
}
