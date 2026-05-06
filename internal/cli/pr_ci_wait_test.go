package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/cli"
)

// fakeCIStatus builds a Forgejo /commits/{sha}/status response for
// the supplied (state, [{name, state}, ...]) tuple. Used by the
// ci-wait tests to drive specific check shapes.
func fakeCIStatus(rollupState string, statuses []map[string]string) map[string]any {
	out := map[string]any{"state": rollupState}
	arr := make([]map[string]any, 0, len(statuses))
	for _, s := range statuses {
		arr = append(arr, map[string]any{
			"status":  s["state"], // Forgejo API field is "status", not "state"
			"context": s["name"],
		})
	}
	out["statuses"] = arr
	return out
}

// runCIWait drives a `gaia pr ci-wait` invocation against a stubbed
// upstream and returns (stdout, stderr, exit code).
func runCIWait(t *testing.T, srv *httptest.Server, args ...string) (string, string, int) {
	t.Helper()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	full := append([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"--repo", "o/r",
		"pr", "ci-wait",
	}, args...)
	root.SetArgs(full)
	err := root.Execute()
	return stdout.String(), stderr.String(), exitcode.Of(err)
}

// TestPRCIWaitAllSuccess verifies the happy path: every check
// already success → exit 0.
func TestPRCIWaitAllSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			_ = json.NewEncoder(w).Encode(fakePRJSON(7, "open", false))
		case strings.HasSuffix(r.URL.Path, "/status"):
			_ = json.NewEncoder(w).Encode(fakeCIStatus("success",
				[]map[string]string{
					{"name": "ci/build", "state": "success"},
					{"name": "ci/lint", "state": "success"},
				}))
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	_, _, code := runCIWait(t, srv, "7", "--interval", "10ms", "--timeout", "1s")
	if code != exitcode.OK {
		t.Errorf("exit: got %d want OK", code)
	}
}

// TestPRCIWaitFailedNonFlaky verifies a real failing check surfaces
// as exitcode.CheckFailed.
func TestPRCIWaitFailedNonFlaky(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			_ = json.NewEncoder(w).Encode(fakePRJSON(7, "open", false))
		case strings.HasSuffix(r.URL.Path, "/status"):
			_ = json.NewEncoder(w).Encode(fakeCIStatus("failure",
				[]map[string]string{
					{"name": "ci/build", "state": "success"},
					{"name": "ci/test", "state": "failure"},
				}))
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	_, stderr, code := runCIWait(t, srv, "7", "--interval", "10ms", "--timeout", "1s")
	if code != exitcode.CheckFailed {
		t.Errorf("exit: got %d want CheckFailed (%d)\nstderr: %s", code, exitcode.CheckFailed, stderr)
	}
}

// TestPRCIWaitFlakyByName verifies a failure on a check whose name
// matches the flaky-marker regex routes to CheckFlaky.
func TestPRCIWaitFlakyByName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			_ = json.NewEncoder(w).Encode(fakePRJSON(7, "open", false))
		case strings.HasSuffix(r.URL.Path, "/status"):
			_ = json.NewEncoder(w).Encode(fakeCIStatus("failure",
				[]map[string]string{
					{"name": "ci/build", "state": "success"},
					{"name": "tests (flaky-rerun)", "state": "failure"},
				}))
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	_, stderr, code := runCIWait(t, srv, "7", "--interval", "10ms", "--timeout", "1s")
	if code != exitcode.CheckFlaky {
		t.Errorf("exit: got %d want CheckFlaky (%d)\nstderr: %s", code, exitcode.CheckFlaky, stderr)
	}
}

// TestPRCIWaitMixedFailureClassifiesAsHard verifies that when at
// least one failing check is non-flaky, the whole result lands in
// CheckFailed (not CheckFlaky). Operators can rely on
// `abort_on: [check_failed]` not being silently demoted by the
// presence of a flaky-named sibling.
func TestPRCIWaitMixedFailureClassifiesAsHard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			_ = json.NewEncoder(w).Encode(fakePRJSON(7, "open", false))
		case strings.HasSuffix(r.URL.Path, "/status"):
			_ = json.NewEncoder(w).Encode(fakeCIStatus("failure",
				[]map[string]string{
					{"name": "ci/build", "state": "failure"},
					{"name": "tests (attempt 2)", "state": "failure"},
				}))
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	_, _, code := runCIWait(t, srv, "7", "--interval", "10ms", "--timeout", "1s")
	if code != exitcode.CheckFailed {
		t.Errorf("exit: got %d want CheckFailed (%d)", code, exitcode.CheckFailed)
	}
}

// TestPRCIWaitTimeoutWhilePending verifies that --timeout reaching
// while checks are still pending surfaces as CheckFlaky (caller is
// expected to wait + retry; chains: yield_on: [check_flaky]).
func TestPRCIWaitTimeoutWhilePending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			_ = json.NewEncoder(w).Encode(fakePRJSON(7, "open", false))
		case strings.HasSuffix(r.URL.Path, "/status"):
			_ = json.NewEncoder(w).Encode(fakeCIStatus("pending",
				[]map[string]string{
					{"name": "ci/build", "state": "pending"},
				}))
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	_, _, code := runCIWait(t, srv, "7", "--interval", "10ms", "--timeout", "30ms")
	if code != exitcode.CheckFlaky {
		t.Errorf("exit: got %d want CheckFlaky (%d)", code, exitcode.CheckFlaky)
	}
}

// TestPRCIWaitPollsUntilSettled verifies the poll loop: first call
// returns pending, subsequent call returns success → exit 0.
func TestPRCIWaitPollsUntilSettled(t *testing.T) {
	var statusCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			_ = json.NewEncoder(w).Encode(fakePRJSON(7, "open", false))
		case strings.HasSuffix(r.URL.Path, "/status"):
			n := statusCalls.Add(1)
			if n == 1 {
				_ = json.NewEncoder(w).Encode(fakeCIStatus("pending",
					[]map[string]string{
						{"name": "ci/build", "state": "pending"},
					}))
				return
			}
			_ = json.NewEncoder(w).Encode(fakeCIStatus("success",
				[]map[string]string{
					{"name": "ci/build", "state": "success"},
				}))
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	_, _, code := runCIWait(t, srv, "7", "--interval", "10ms", "--timeout", "1s")
	if code != exitcode.OK {
		t.Errorf("exit: got %d want OK", code)
	}
	if statusCalls.Load() < 2 {
		t.Errorf("expected at least 2 polls, got %d", statusCalls.Load())
	}
}

// TestPRCIWaitRefSuccess verifies --ref polls /commits/{ref}/status
// without needing a PR number, and exits 0 when checks succeed.
func TestPRCIWaitRefSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/status") {
			t.Errorf("unexpected path: %q", r.URL.Path)
			return
		}
		_ = json.NewEncoder(w).Encode(fakeCIStatus("success",
			[]map[string]string{
				{"name": "release / publish", "state": "success"},
			}))
	}))
	defer srv.Close()

	_, _, code := runCIWait(t, srv, "--ref", "v0.2.7", "--interval", "10ms", "--timeout", "1s")
	if code != exitcode.OK {
		t.Errorf("exit: got %d want OK", code)
	}
}

// TestPRCIWaitRefEmptyStateThenSuccess verifies the polling loop
// handles empty state (no checks registered yet) as pending and keeps
// polling until a real state appears.
func TestPRCIWaitRefEmptyStateThenSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/status") {
			t.Errorf("unexpected path: %q", r.URL.Path)
			return
		}
		n := calls.Add(1)
		if n == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "", "statuses": []any{}})
			return
		}
		_ = json.NewEncoder(w).Encode(fakeCIStatus("success",
			[]map[string]string{{"name": "release", "state": "success"}}))
	}))
	defer srv.Close()

	_, _, code := runCIWait(t, srv, "--ref", "v0.2.7", "--interval", "10ms", "--timeout", "2s")
	if code != exitcode.OK {
		t.Errorf("exit: got %d want OK", code)
	}
	if calls.Load() < 2 {
		t.Errorf("expected at least 2 polls, got %d", calls.Load())
	}
}

// TestPRCIWaitRefFailure verifies --ref polls exit CheckFailed when
// a non-flaky check fails.
func TestPRCIWaitRefFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/status") {
			t.Errorf("unexpected path: %q", r.URL.Path)
			return
		}
		_ = json.NewEncoder(w).Encode(fakeCIStatus("failure",
			[]map[string]string{
				{"name": "release / publish", "state": "failure"},
			}))
	}))
	defer srv.Close()

	_, _, code := runCIWait(t, srv, "--ref", "v0.2.7", "--interval", "10ms", "--timeout", "1s")
	if code != exitcode.CheckFailed {
		t.Errorf("exit: got %d want CheckFailed", code)
	}
}

// TestPRCIWaitRefAndNumberMutuallyExclusive verifies that passing both
// a PR number and --ref is rejected at usage time.
func TestPRCIWaitRefAndNumberMutuallyExclusive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	_, _, code := runCIWait(t, srv, "42", "--ref", "v0.2.7", "--interval", "10ms")
	if code != exitcode.Usage {
		t.Errorf("exit: got %d want Usage", code)
	}
}

// TestPRCIWaitFlakyMarkerExtra verifies --flaky-marker adds
// custom name substrings to the flakiness classifier.
func TestPRCIWaitFlakyMarkerExtra(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			_ = json.NewEncoder(w).Encode(fakePRJSON(7, "open", false))
		case strings.HasSuffix(r.URL.Path, "/status"):
			_ = json.NewEncoder(w).Encode(fakeCIStatus("failure",
				[]map[string]string{
					{"name": "e2e-canary", "state": "failure"},
				}))
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	// Without the marker → CheckFailed.
	_, _, code := runCIWait(t, srv, "7", "--interval", "10ms", "--timeout", "1s")
	if code != exitcode.CheckFailed {
		t.Errorf("baseline: exit got %d want CheckFailed", code)
	}

	// With "canary" added as a flaky marker → CheckFlaky.
	_, _, code = runCIWait(t, srv, "7", "--interval", "10ms", "--timeout", "1s", "--flaky-marker", "canary")
	if code != exitcode.CheckFlaky {
		t.Errorf("with marker: exit got %d want CheckFlaky", code)
	}
}
