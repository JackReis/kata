package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProductTreeSatisfiesPolicy(t *testing.T) {
	root := repoRoot(t)
	code, stderr := runGate(t, root, "")
	assert.Zero(t, code, stderr)
	assert.Empty(t, stderr)
}

func TestBannedGoEdgeExitsOne(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/spoke\n\ngo 1.27.0\n")
	writeFile(t, root, "internal/githubsync/sync.go", `package githubsync

import "example.com/spoke/internal/federation"

func Sync() { _ = federationPlaceholder }

var federationPlaceholder = 1
`)
	writeFile(t, root, "internal/federation/federation.go", "package federation\n")
	policy := `{
	  "version": 1,
	  "fail_closed": true,
	  "scopes": [{"id": "go", "language": "go", "root": "."}],
	  "denies": [{
	    "id": "githubsync-not-federation",
	    "scope": "go",
	    "from": "example.com/spoke/internal/githubsync",
	    "to": ["example.com/spoke/internal/federation"],
	    "reason": "Feature colocation: GitHub sync must not import federation."
	  }]
	}`

	code, stderr := runGate(t, root, writeFile(t, root, "policy.json", policy))
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "banned edge")
	assert.Contains(t, stderr, "githubsync-not-federation")
	assert.Contains(t, stderr, "example.com/spoke/internal/federation")
	assert.Contains(t, stderr, "internal/githubsync/sync.go")
}

func TestCleanGoEdgeExitsZero(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/spoke\n\ngo 1.27.0\n")
	writeFile(t, root, "internal/githubsync/sync.go", "package githubsync\n\nfunc Sync() {}\n")
	policy := `{
	  "version": 1,
	  "fail_closed": true,
	  "scopes": [{"id": "go", "language": "go", "root": "."}],
	  "denies": [{
	    "id": "githubsync-not-federation",
	    "scope": "go",
	    "from": "example.com/spoke/internal/githubsync",
	    "to": ["example.com/spoke/internal/federation"],
	    "reason": "Feature colocation: GitHub sync must not import federation."
	  }]
	}`

	code, stderr := runGate(t, root, writeFile(t, root, "policy.json", policy))
	assert.Zero(t, code, stderr)
	assert.Empty(t, stderr)
}

func TestTestOnlyImportIsNotAProductEdge(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/spoke\n\ngo 1.27.0\n")
	writeFile(t, root, "internal/client/client.go", "package client\n\nfunc Dial() {}\n")
	writeFile(t, root, "internal/client/client_test.go", `package client

import "example.com/banned/store"

func TestDial(t *testing.T) { _ = store.Open }
`)
	policy := `{
	  "version": 1,
	  "fail_closed": true,
	  "scopes": [{"id": "go", "language": "go", "root": "."}],
	  "denies": [{
	    "id": "client-no-store",
	    "scope": "go",
	    "from": "example.com/spoke/internal/client",
	    "to": ["example.com/banned/store"],
	    "reason": "Clients must not open the store."
	  }]
	}`

	code, stderr := runGate(t, root, writeFile(t, root, "policy.json", policy))
	assert.Zero(t, code, stderr)
	assert.Empty(t, stderr)
}

func TestBrowserNodeImportExitsOne(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "web/src/lib/auth/session.ts", "import { readFile } from 'node:fs'\nexport const session = readFile\n")
	writeFile(t, root, "web/scripts/dev.ts", "import { readFile } from 'node:fs'\n")
	policy := `{
	  "version": 1,
	  "fail_closed": true,
	  "scopes": [{"id": "browser", "language": "ts", "root": "web/src"}],
	  "denies": [{
	    "id": "browser-no-node",
	    "scope": "browser",
	    "from": "web/src",
	    "to": ["node:"],
	    "reason": "Browser UI must not import Node side-effect modules."
	  }]
	}`

	code, stderr := runGate(t, root, writeFile(t, root, "policy.json", policy))
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "browser-no-node")
	assert.Contains(t, stderr, "node:fs")
	assert.Contains(t, stderr, "web/src/lib/auth/session.ts")
	assert.NotContains(t, stderr, "web/scripts/dev.ts")
}

func TestRelativeSpecifierDoesNotMatchBareBuiltin(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "web/src/app.ts", "import { name } from './fs'\nexport const label = name\n")
	writeFile(t, root, "web/src/fs.ts", "export const name = 'fs'\n")
	policy := `{
	  "version": 1,
	  "fail_closed": true,
	  "scopes": [{"id": "browser", "language": "ts", "root": "web/src"}],
	  "denies": [{
	    "id": "browser-no-node",
	    "scope": "browser",
	    "from": "web/src",
	    "to": ["fs", "node:"],
	    "reason": "Browser UI must not import Node side-effect modules."
	  }]
	}`

	code, stderr := runGate(t, root, writeFile(t, root, "policy.json", policy))
	assert.Zero(t, code, stderr)
	assert.Empty(t, stderr)
}

func TestBareNodeBuiltinExitsOne(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "web/src/app.ts", "import fs from 'fs'\nexport const read = fs.readFile\n")
	policy := `{
	  "version": 1,
	  "fail_closed": true,
	  "scopes": [{"id": "browser", "language": "ts", "root": "web/src"}],
	  "denies": [{
	    "id": "browser-no-fs",
	    "scope": "browser",
	    "from": "web/src",
	    "to": ["fs"],
	    "reason": "Browser UI must not import Node side-effect modules."
	  }]
	}`

	code, stderr := runGate(t, root, writeFile(t, root, "policy.json", policy))
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "browser-no-fs")
	assert.Contains(t, stderr, "'fs'")
}

func TestSvelteScriptImportIsGated(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "web/src/App.svelte", `<script lang="ts">
  import { spawn } from 'node:child_process'
  export const start = spawn
</script>
<p>from "node:fs" is markup, not an import</p>
`)
	policy := `{
	  "version": 1,
	  "fail_closed": true,
	  "scopes": [{"id": "browser", "language": "ts", "root": "web/src"}],
	  "denies": [{
	    "id": "browser-no-node",
	    "scope": "browser",
	    "from": "web/src",
	    "to": ["node:"],
	    "reason": "Browser UI must not import Node side-effect modules."
	  }]
	}`

	code, stderr := runGate(t, root, writeFile(t, root, "policy.json", policy))
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "node:child_process")
	assert.NotContains(t, stderr, "node:fs")
}

func TestMissingPolicyFailsClosed(t *testing.T) {
	root := t.TempDir()
	code, stderr := runGate(t, root, filepath.Join(root, "missing-policy.json"))
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "FAIL-closed")
	assert.Contains(t, stderr, "policy")
}

func TestFailClosedFalseIsRejected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/spoke\n\ngo 1.27.0\n")
	writeFile(t, root, "pkg.go", "package spoke\n")
	policy := `{
	  "version": 1,
	  "fail_closed": false,
	  "scopes": [{"id": "go", "language": "go", "root": "."}],
	  "denies": [{
	    "id": "alive",
	    "scope": "go",
	    "from": "example.com/spoke",
	    "to": ["example.com/banned"],
	    "reason": "placeholder"
	  }]
	}`

	code, stderr := runGate(t, root, writeFile(t, root, "policy.json", policy))
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "FAIL-closed")
	assert.Contains(t, stderr, "fail_closed")
}

func TestEmptyDeniesFailClosed(t *testing.T) {
	root := t.TempDir()
	policy := `{
	  "version": 1,
	  "fail_closed": true,
	  "scopes": [{"id": "go", "language": "go", "root": "."}],
	  "denies": []
	}`

	code, stderr := runGate(t, root, writeFile(t, root, "policy.json", policy))
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "FAIL-closed")
	assert.Contains(t, stderr, "denies")
}

func TestDeadFromFailsClosed(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/spoke\n\ngo 1.27.0\n")
	writeFile(t, root, "pkg.go", "package spoke\n")
	policy := `{
	  "version": 1,
	  "fail_closed": true,
	  "scopes": [{"id": "go", "language": "go", "root": "."}],
	  "denies": [{
	    "id": "missing-package",
	    "scope": "go",
	    "from": "example.com/spoke/internal/missing",
	    "to": ["example.com/banned"],
	    "reason": "This from prefix matches nothing."
	  }]
	}`

	code, stderr := runGate(t, root, writeFile(t, root, "policy.json", policy))
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "FAIL-closed")
	assert.Contains(t, stderr, "missing-package")
	assert.Contains(t, stderr, "example.com/spoke/internal/missing")
}

func TestZeroProductionFilesFailClosed(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/spoke\n\ngo 1.27.0\n")
	writeFile(t, root, "only_test.go", "package spoke\n")
	policy := `{
	  "version": 1,
	  "fail_closed": true,
	  "scopes": [{"id": "go", "language": "go", "root": "."}],
	  "denies": [{
	    "id": "alive",
	    "scope": "go",
	    "from": "example.com/spoke",
	    "to": ["example.com/banned"],
	    "reason": "placeholder"
	  }]
	}`

	code, stderr := runGate(t, root, writeFile(t, root, "policy.json", policy))
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "FAIL-closed")
	assert.Contains(t, stderr, "no production")
}

func TestExceptFromAllowsCompositionRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/spoke\n\ngo 1.27.0\n")
	writeFile(t, root, "service.go", `package spoke

import "example.com/spoke/internal/db/storeopen"

func Open() { _ = storeopen.Open }
`)
	writeFile(t, root, "internal/db/storeopen/storeopen.go", "package storeopen\n\nfunc Open() {}\n")
	writeFile(t, root, "internal/tui/tui.go", `package tui

import "example.com/spoke/internal/db/storeopen"

func View() { _ = storeopen.Open }
`)
	policy := `{
	  "version": 1,
	  "fail_closed": true,
	  "scopes": [{"id": "go", "language": "go", "root": "."}],
	  "denies": [{
	    "id": "storeopen-allowlist",
	    "scope": "go",
	    "from": "example.com/spoke",
	    "to": ["example.com/spoke/internal/db/storeopen"],
	    "except_from_exact": ["example.com/spoke"],
	    "reason": "Only the composition root may open the store."
	  }]
	}`

	code, stderr := runGate(t, root, writeFile(t, root, "policy.json", policy))
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "internal/tui/tui.go")
	assert.NotContains(t, stderr, "service.go")
}

func TestBinaryExitCodes(t *testing.T) {
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), "duneimport")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = filepath.Join(root, "tools", "duneimport")
	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))

	missingRoot := t.TempDir()
	var missingErr bytes.Buffer
	missing := exec.Command(bin, "-root", missingRoot, "-policy", filepath.Join(missingRoot, "missing.json"))
	missing.Stderr = &missingErr
	require.Equal(t, 2, commandExit(t, missing), missingErr.String())
	assert.Contains(t, missingErr.String(), "FAIL-closed")
	assert.Contains(t, missingErr.String(), "policy")

	bannedRoot := t.TempDir()
	writeFile(t, bannedRoot, "go.mod", "module example.com/spoke\n\ngo 1.27.0\n")
	writeFile(t, bannedRoot, "internal/githubsync/sync.go", "package githubsync\n\nimport \"example.com/spoke/internal/federation\"\n")
	writeFile(t, bannedRoot, "internal/federation/federation.go", "package federation\n")
	policy := writeFile(t, bannedRoot, "policy.json", `{
	  "version": 1,
	  "fail_closed": true,
	  "scopes": [{"id": "go", "language": "go", "root": "."}],
	  "denies": [{
	    "id": "githubsync-not-federation",
	    "scope": "go",
	    "from": "example.com/spoke/internal/githubsync",
	    "to": ["example.com/spoke/internal/federation"],
	    "reason": "Feature colocation: GitHub sync must not import federation."
	  }]
	}`)
	var bannedErr bytes.Buffer
	banned := exec.Command(bin, "-root", bannedRoot, "-policy", policy)
	banned.Stderr = &bannedErr
	require.Equal(t, 1, commandExit(t, banned), bannedErr.String())
	assert.Contains(t, bannedErr.String(), "internal/githubsync/sync.go")
	assert.Contains(t, bannedErr.String(), "example.com/spoke/internal/federation")
}

func commandExit(t *testing.T, cmd *exec.Cmd) int {
	t.Helper()
	err := cmd.Run()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	return exitErr.ExitCode()
}

func TestMatchPattern(t *testing.T) {
	assert.True(t, matchPattern("go.example/internal/db/sqlitestore", "go.example/internal/db"))
	assert.False(t, matchPattern("go.example/internal/dbtest", "go.example/internal/db"))
	assert.True(t, matchPattern("node:fs", "node:"))
	assert.False(t, matchPattern("./fs", "fs"))
	assert.False(t, matchPattern("fs-extra", "fs"))
	assert.True(t, matchPattern("fs/promises", "fs"))
	assert.False(t, matchPattern("https", "http"))
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir)
		dir = parent
	}
}

func runGate(t *testing.T, root, policyPath string) (int, string) {
	t.Helper()
	var stderr bytes.Buffer
	code := run(&stderr, root, policyPath)
	return code, stderr.String()
}

func writeFile(t *testing.T, root, rel, body string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}
