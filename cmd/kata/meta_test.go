package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.kenn.io/kata/internal/testenv"
)

func TestMetaSetStoresJSONStringAndGetReturnsValue(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "metadata string issue")

	out := runCLI(t, env, dir, "meta", "set", ref, "work.attention", "needs-human")
	assert.Contains(t, out, "set")
	assert.Contains(t, out, ref)
	assert.Contains(t, out, "work.attention")
	assert.Contains(t, out, "rev-2")

	issue := fetchMetaIssueViaHTTP(t, env, pid, ref)
	require.JSONEq(t, `{"work.attention":"needs-human"}`, string(issue.Issue.Metadata))

	getOut := runCLI(t, env, dir, "meta", "get", ref, "work.attention")
	assert.Equal(t, "needs-human", getOut)
}

func TestMetaSetJSONValueObjectRoundTrips(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "metadata object issue")

	runCLI(t, env, dir, "meta", "set", "--json-value", ref, "work.state", `{"attention":"ok","count":2}`)

	issue := fetchMetaIssueViaHTTP(t, env, pid, ref)
	require.JSONEq(t, `{"work.state":{"attention":"ok","count":2}}`, string(issue.Issue.Metadata))

	out := runCLI(t, env, dir, "meta", "get", "--json", ref, "work.state")
	assert.JSONEq(t, `{"attention":"ok","count":2}`, out)
}

func TestMetaSetJSONValueRejectsInvalidJSONClientSide(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "invalid json metadata issue")

	_, stderr, err := runCLIWithErr(t, env, dir, "meta", "set", "--json-value", ref, "work.state", `{"broken"`)
	ce := requireCLIError(t, err, ExitValidation)
	assert.Equal(t, kindValidation, ce.Kind)
	assert.Contains(t, stderr, "invalid JSON")

	issue := fetchMetaIssueViaHTTP(t, env, pid, ref)
	assert.JSONEq(t, `{}`, string(issue.Issue.Metadata))
}

func TestMetaSetJSONValueRejectsNullWithUnsetHint(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "null metadata issue")

	_, stderr, err := runCLIWithErr(t, env, dir, "meta", "set", "--json-value", ref, "work.state", `null`)
	ce := requireCLIError(t, err, ExitValidation)
	assert.Equal(t, kindValidation, ce.Kind)
	assert.Contains(t, stderr, "use `kata meta unset`")
}

func TestMetaUnsetRemovesKey(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "unset metadata issue")

	runCLI(t, env, dir, "meta", "set", ref, "work.branch", "feature/example")
	out := runCLI(t, env, dir, "meta", "unset", ref, "work.branch")
	assert.Contains(t, out, "unset")
	assert.Contains(t, out, "work.branch")
	assert.Contains(t, out, "rev-3")

	issue := fetchMetaIssueViaHTTP(t, env, pid, ref)
	assert.JSONEq(t, `{}`, string(issue.Issue.Metadata))
}

func TestMetaGetEmptyAndMissingKey(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "empty metadata issue")

	out := runCLI(t, env, dir, "meta", "get", ref)
	assert.Contains(t, out, "no metadata")

	_, stderr, err := runCLIWithErr(t, env, dir, "meta", "get", ref, "work.missing")
	ce := requireCLIError(t, err, ExitNotFound)
	assert.Equal(t, kindNotFound, ce.Kind)
	assert.Contains(t, stderr, "metadata key not found")
}

func TestMetaReservedKeyRejectionSurfacesValidation(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "reserved metadata issue")

	_, stderr, err := runCLIWithErr(t, env, dir, "meta", "set", ref, "scheduled_on", "not-a-date")
	ce := requireCLIError(t, err, ExitValidation)
	assert.Equal(t, kindValidation, ce.Kind)
	assert.Contains(t, stderr, "invalid_metadata_value")
}

func TestMetaIfMatchStaleRevisionConflictsAndCorrectRevisionSucceeds(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "if match metadata issue")

	_, stderr, err := runCLIWithErr(t, env, dir, "meta", "set", "--if-match", "rev-99", ref, "work.attention", "ok")
	ce := requireCLIError(t, err, ExitConfirm)
	assert.Equal(t, kindConfirm, ce.Kind)
	assert.Contains(t, stderr, "revision conflict")

	out := runCLI(t, env, dir, "meta", "set", "--if-match", "1", ref, "work.attention", "ok")
	assert.Contains(t, out, "rev-2")
}

func TestMetaSetAndGetAgentOutput(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "agent metadata issue")

	setOut := runCLI(t, env, dir, "--agent", "meta", "set", ref, "work.attention", "stuck")
	assert.Contains(t, setOut, "OK meta set")
	assert.Contains(t, setOut, "key=work.attention")
	assert.Contains(t, setOut, "revision=rev-2")

	getOut := runCLI(t, env, dir, "--agent", "meta", "get", ref)
	assert.Contains(t, getOut, "OK meta get")
	assert.Contains(t, getOut, "key=work.attention")
	assert.Contains(t, getOut, `value="stuck"`)
}

func TestMetaSetAndGetJSONOutput(t *testing.T) {
	env, dir, pid := setupCLIWorkspace(t)
	ref := createIssue(t, env, pid, "json metadata issue")

	setOut := runCLI(t, env, dir, "--json", "meta", "set", ref, "work.attention", "ok")
	var setPayload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(setOut), &setPayload))
	assert.Contains(t, string(setPayload["issue"]), `"short_id":"`+ref+`"`)
	assert.Contains(t, string(setPayload["issue"]), `"revision":2`)

	getOut := runCLI(t, env, dir, "--json", "meta", "get", ref)
	assert.JSONEq(t, `{"work.attention":"ok"}`, getOut)
}

type metaIssueResponse struct {
	Issue struct {
		ShortID  string          `json:"short_id"`
		Metadata json.RawMessage `json:"metadata"`
		Revision int64           `json:"revision"`
	} `json:"issue"`
}

func fetchMetaIssueViaHTTP(t *testing.T, env *testenv.Env, pid int64, ref string) metaIssueResponse {
	t.Helper()
	issue := getJSON[metaIssueResponse](t, env.URL+"/api/v1/projects/"+itoa(pid)+"/issues/"+ref)
	if len(strings.TrimSpace(string(issue.Issue.Metadata))) == 0 {
		issue.Issue.Metadata = json.RawMessage(`{}`)
	}
	return issue
}
