package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.kenn.io/kata/internal/testenv"
)

// --- pure condition-evaluation unit tests -------------------------------

func TestWaitEvalCondition(t *testing.T) {
	cases := []struct {
		name       string
		mode       waitMode
		st         issueState
		wantSat    bool
		wantReason string
	}{
		{"closed-mode-open-pending", waitClosed, issueState{status: "open"}, false, ""},
		{"closed-mode-closed", waitClosed, issueState{status: "closed"}, true, "closed"},
		{"needs-human-open-no-attention", waitNeedsHuman, issueState{status: "open"}, false, ""},
		{"needs-human-open-ok", waitNeedsHuman, issueState{status: "open", attention: "ok"}, false, ""},
		{"needs-human-match", waitNeedsHuman, issueState{status: "open", attention: "needs-human"}, true, "attention"},
		{"needs-human-stuck-no-match", waitNeedsHuman, issueState{status: "open", attention: "stuck"}, false, ""},
		{"stuck-match", waitStuck, issueState{status: "open", attention: "stuck"}, true, "attention"},
		{"stuck-needs-human-no-match", waitStuck, issueState{status: "open", attention: "needs-human"}, false, ""},
		{"attention-ok-no-match", waitAttention, issueState{status: "open", attention: "ok"}, false, ""},
		{"attention-empty-no-match", waitAttention, issueState{status: "open", attention: ""}, false, ""},
		{"attention-needs-human", waitAttention, issueState{status: "open", attention: "needs-human"}, true, "attention"},
		{"attention-stuck", waitAttention, issueState{status: "open", attention: "stuck"}, true, "attention"},
		{"attention-novel-level", waitAttention, issueState{status: "open", attention: "on-fire"}, true, "attention"},
		{"needs-human-but-closed", waitNeedsHuman, issueState{status: "closed"}, true, "closed"},
		{"attention-but-closed", waitAttention, issueState{status: "closed", attention: "ok"}, true, "closed"},
		{"stuck-but-closed", waitStuck, issueState{status: "closed", attention: "stuck"}, true, "closed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sat, reason := evalWait(c.mode, c.st)
			assert.Equal(t, c.wantSat, sat)
			assert.Equal(t, c.wantReason, reason)
		})
	}
}

func TestWaitParseModeRejectsUnknown(t *testing.T) {
	_, err := parseWaitMode("banana")
	require.Error(t, err)
	requireCLIError(t, err, ExitValidation)

	for _, ok := range []string{"closed", "attention", "needs-human", "stuck"} {
		m, err := parseWaitMode(ok)
		require.NoError(t, err)
		assert.Equal(t, waitMode(ok), m)
	}
}

// --- integration tests --------------------------------------------------

const (
	waitFastPoll  = "25ms"
	waitSafetyNet = "5s"
	waitMutDelay  = 120 * time.Millisecond
)

func TestWaitAlreadyClosedReturnsImmediately(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "finished work")
	require.NoError(t, closeIssueHTTP(env, pid, ref))

	start := time.Now()
	stdout, _, err := runCLIWithErr(t, env, dir,
		"wait", ref, "--poll-interval", waitFastPoll, "--timeout", waitSafetyNet)
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 2*time.Second, "already-closed should not enter the poll loop")
	assert.Contains(t, stdout, ref)
	assert.Contains(t, stdout, "closed")
}

func TestWaitClosesAfterDelay(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "will close soon")

	errc := make(chan error, 1)
	go func() {
		time.Sleep(waitMutDelay)
		errc <- closeIssueHTTP(env, pid, ref)
	}()

	stdout, _, err := runCLIWithErr(t, env, dir,
		"wait", ref, "--poll-interval", waitFastPoll, "--timeout", waitSafetyNet)
	require.NoError(t, <-errc)
	require.NoError(t, err)
	assert.Contains(t, stdout, ref)
	assert.Contains(t, stdout, "closed")
}

func TestWaitUntilNeedsHumanSurfacesMessage(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "needs a human")

	errc := make(chan error, 1)
	go func() {
		time.Sleep(waitMutDelay)
		errc <- setAttentionHTTP(env, pid, ref, "needs-human", "blocked on database migration")
	}()

	stdout, _, err := runCLIWithErr(t, env, dir,
		"wait", ref, "--until", "needs-human", "--poll-interval", waitFastPoll, "--timeout", waitSafetyNet)
	require.NoError(t, <-errc)
	require.NoError(t, err)
	assert.Contains(t, stdout, "needs-human")
	assert.Contains(t, stdout, "blocked on database migration")
}

func TestWaitUntilAttentionFiresOnStuck(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "stuck work")

	errc := make(chan error, 1)
	go func() {
		time.Sleep(waitMutDelay)
		errc <- setAttentionHTTP(env, pid, ref, "stuck", "")
	}()

	stdout, _, err := runCLIWithErr(t, env, dir,
		"wait", ref, "--until", "attention", "--poll-interval", waitFastPoll, "--timeout", waitSafetyNet)
	require.NoError(t, <-errc)
	require.NoError(t, err)
	assert.Contains(t, stdout, "stuck")
}

func TestWaitUntilAttentionFiresOnNovelLevel(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "novel level")

	errc := make(chan error, 1)
	go func() {
		time.Sleep(waitMutDelay)
		errc <- setAttentionHTTP(env, pid, ref, "on-fire", "")
	}()

	stdout, _, err := runCLIWithErr(t, env, dir,
		"wait", ref, "--until", "attention", "--poll-interval", waitFastPoll, "--timeout", waitSafetyNet)
	require.NoError(t, <-errc)
	require.NoError(t, err)
	assert.Contains(t, stdout, "on-fire")
}

func TestWaitAttentionModeCompletesOnClose(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "closes instead of flagging")

	errc := make(chan error, 1)
	go func() {
		time.Sleep(waitMutDelay)
		errc <- closeIssueHTTP(env, pid, ref)
	}()

	stdout, _, err := runCLIWithErr(t, env, dir,
		"wait", ref, "--until", "needs-human", "--poll-interval", waitFastPoll, "--timeout", waitSafetyNet)
	require.NoError(t, <-errc)
	require.NoError(t, err)
	assert.Contains(t, stdout, "closed")
}

func TestWaitAnyReturnsOnFirst(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref1 := createIssue(t, env, pid, "first ref")
	ref2 := createIssue(t, env, pid, "second ref never closes")

	errc := make(chan error, 1)
	go func() {
		time.Sleep(waitMutDelay)
		errc <- closeIssueHTTP(env, pid, ref1)
	}()

	// --timeout is a safety net; if --any did not short-circuit, ref2 never
	// closes and the command would time out (nonzero exit) instead.
	stdout, _, err := runCLIWithErr(t, env, dir,
		"wait", ref1, ref2, "--any", "--poll-interval", waitFastPoll, "--timeout", waitSafetyNet)
	require.NoError(t, <-errc)
	require.NoError(t, err)
	assert.Contains(t, stdout, ref1)
}

func TestWaitAllWaitsForEvery(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref1 := createIssue(t, env, pid, "first of two")
	ref2 := createIssue(t, env, pid, "second of two")

	errc := make(chan error, 1)
	go func() {
		e1 := (error)(nil)
		time.Sleep(waitMutDelay)
		e1 = closeIssueHTTP(env, pid, ref1)
		time.Sleep(waitMutDelay)
		e2 := closeIssueHTTP(env, pid, ref2)
		errc <- errors.Join(e1, e2)
	}()

	stdout, _, err := runCLIWithErr(t, env, dir,
		"wait", ref1, ref2, "--all", "--poll-interval", waitFastPoll, "--timeout", waitSafetyNet)
	require.NoError(t, <-errc)
	require.NoError(t, err)
	assert.Contains(t, stdout, ref1)
	assert.Contains(t, stdout, ref2)
}

func TestWaitTimeoutReportsPendingAndExitCode(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "never changes")

	_, stderr, err := runCLIWithErr(t, env, dir,
		"wait", ref, "--poll-interval", waitFastPoll, "--timeout", "200ms")
	requireCLIError(t, err, ExitWaitTimeout)
	assert.Contains(t, stderr, "pending")
	assert.Contains(t, stderr, ref)
}

func TestWaitTimeoutJSONEmitsObject(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "never changes json")

	stdout, _, err := runCLIWithErr(t, env, dir,
		"--json", "wait", ref, "--poll-interval", waitFastPoll, "--timeout", "200ms")
	requireCLIError(t, err, ExitWaitTimeout)

	obj := parseWaitJSON(t, stdout)
	assert.True(t, obj.TimedOut)
	assert.Contains(t, obj.Pending, ref)
	assert.Empty(t, obj.Results)
}

func TestWaitUsageAnyAllConflict(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "conflict")

	_, err := runCLICapture(t, env, dir, "wait", ref, "--any", "--all")
	requireCLIError(t, err, ExitUsage)
}

func TestWaitUsageZeroPollInterval(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "bad poll")

	_, err := runCLICapture(t, env, dir, "wait", ref, "--poll-interval", "0s")
	requireCLIError(t, err, ExitValidation)
}

func TestWaitBadRefFailsFast(t *testing.T) {
	env, dir, _ := setupCLIWorkspace(t)

	start := time.Now()
	_, err := runCLICapture(t, env, dir,
		"wait", "zzzz", "--poll-interval", waitFastPoll, "--timeout", waitSafetyNet)
	require.Error(t, err)
	requireCLIError(t, err, ExitNotFound)
	assert.Less(t, time.Since(start), 2*time.Second, "bad ref must fail before entering the poll loop")
}

func TestWaitAnySucceedsWhenOtherRefAlreadySatisfiedBadRefLast(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "already closed, bad ref last")
	require.NoError(t, closeIssueHTTP(env, pid, ref))

	start := time.Now()
	stdout, _, err := runCLIWithErr(t, env, dir,
		"wait", ref, "zzzz", "--any", "--poll-interval", waitFastPoll, "--timeout", waitSafetyNet)
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 2*time.Second, "already-satisfied --any join should not enter the poll loop")
	assert.Contains(t, stdout, ref)
	assert.Contains(t, stdout, "closed")
}

func TestWaitAnySucceedsWhenOtherRefAlreadySatisfiedBadRefFirst(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "already closed, bad ref first")
	require.NoError(t, closeIssueHTTP(env, pid, ref))

	start := time.Now()
	stdout, _, err := runCLIWithErr(t, env, dir,
		"wait", "zzzz", ref, "--any", "--poll-interval", waitFastPoll, "--timeout", waitSafetyNet)
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 2*time.Second, "already-satisfied --any join should not enter the poll loop")
	assert.Contains(t, stdout, ref)
	assert.Contains(t, stdout, "closed")
}

func TestWaitAnyStillFailsFastOnBadRefWhenJoinUnsatisfied(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "still open, never closes")

	start := time.Now()
	_, err := runCLICapture(t, env, dir,
		"wait", ref, "zzzz", "--any", "--poll-interval", waitFastPoll, "--timeout", waitSafetyNet)
	require.Error(t, err)
	requireCLIError(t, err, ExitNotFound)
	assert.Less(t, time.Since(start), 2*time.Second, "bad ref must fail before entering the poll loop")
}

func TestWaitAllStillFailsOnBadRefEvenWhenOtherRefAlreadySatisfied(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "already closed, but --all with bad ref")
	require.NoError(t, closeIssueHTTP(env, pid, ref))

	_, err := runCLICapture(t, env, dir,
		"wait", ref, "zzzz", "--all", "--poll-interval", waitFastPoll, "--timeout", waitSafetyNet)
	require.Error(t, err)
	requireCLIError(t, err, ExitNotFound)
}

func TestWaitAgentOutput(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "agent output")
	require.NoError(t, closeIssueHTTP(env, pid, ref))

	stdout, _, err := runCLIWithErr(t, env, dir,
		"--agent", "wait", ref, "--poll-interval", waitFastPoll, "--timeout", waitSafetyNet)
	require.NoError(t, err)
	assert.Contains(t, stdout, "OK wait")
	assert.Contains(t, stdout, ref)
	assert.Contains(t, stdout, "reason=closed")
}

func TestWaitJSONOutput(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "json output")
	require.NoError(t, setAttentionHTTP(env, pid, ref, "needs-human", "please look"))

	stdout, _, err := runCLIWithErr(t, env, dir,
		"--json", "wait", ref, "--until", "needs-human", "--poll-interval", waitFastPoll, "--timeout", waitSafetyNet)
	require.NoError(t, err)

	obj := parseWaitJSON(t, stdout)
	assert.False(t, obj.TimedOut)
	assert.Empty(t, obj.Pending)
	require.Len(t, obj.Results, 1)
	assert.Equal(t, ref, obj.Results[0].Ref)
	assert.Equal(t, "attention", obj.Results[0].Reason)
	assert.Equal(t, "needs-human", obj.Results[0].Attention)
	assert.Equal(t, "please look", obj.Results[0].AttentionMsg)
	assert.GreaterOrEqual(t, obj.Results[0].WaitedMs, int64(0))
}

// --- test-local HTTP seeding helpers (goroutine-safe: no *testing.T) ----

// parseWaitJSON decodes the --json wait payload from stdout.
func parseWaitJSON(t *testing.T, stdout string) waitJSONOutput {
	t.Helper()
	var obj waitJSONOutput
	require.NoErrorf(t, json.Unmarshal([]byte(strings.TrimSpace(stdout)), &obj),
		"wait --json output must be parseable; got %q", stdout)
	return obj
}

// closeIssueHTTP closes an issue via the daemon's close action using the TUI
// bypass (reason=done, no evidence). Returns an error instead of failing a
// *testing.T so it is safe to call from a background goroutine.
func closeIssueHTTP(env *testenv.Env, pid int64, ref string) error {
	return postJSONExpectOK(
		fmt.Sprintf("%s/api/v1/projects/%d/issues/%s/actions/close", env.URL, pid, ref),
		map[string]any{"actor": "tester", "source": "tui", "reason": "done"}, "")
}

// setAttentionHTTP patches the work.attention (and optional work.attention_msg)
// metadata keys. It first reads the current revision for the required If-Match
// precondition. Goroutine-safe (returns error, no *testing.T).
func setAttentionHTTP(env *testenv.Env, pid int64, ref, value, msg string) error {
	rev, err := issueRevisionHTTP(env, pid, ref)
	if err != nil {
		return err
	}
	patch := map[string]any{"work.attention": value}
	if msg != "" {
		patch["work.attention_msg"] = msg
	}
	return postJSONExpectOK(
		fmt.Sprintf("%s/api/v1/projects/%d/issues/%s/metadata", env.URL, pid, ref),
		map[string]any{"actor": "tester", "patch": patch},
		fmt.Sprintf(`"rev-%d"`, rev))
}

// issueRevisionHTTP GETs an issue and returns its current revision.
func issueRevisionHTTP(env *testenv.Env, pid int64, ref string) (int64, error) {
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/projects/%d/issues/%s", env.URL, pid, ref)) //nolint:noctx,gosec // test-only loopback
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("GET issue: %d %s", resp.StatusCode, body)
	}
	var out struct {
		Issue struct {
			Revision int64 `json:"revision"`
		} `json:"issue"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.Issue.Revision, nil
}

// postJSONExpectOK POSTs body as JSON, optionally with an If-Match header, and
// returns an error unless the daemon answers 200.
func postJSONExpectOK(url string, body any, ifMatch string) error {
	bs, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(bs))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // test-only loopback
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: %d %s", url, resp.StatusCode, respBody)
	}
	return nil
}
