package importban

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserNodeBuiltinIsBanned(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/main.ts": "import fs from 'node:fs'\n",
	})

	violations, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 {
		t.Fatalf("violations = %#v", violations)
	}
	got := violations[0]
	if got.Rule != "browser-node-builtin" || got.Specifier != "node:fs" || got.File != "web/src/main.ts" || got.Line != 1 {
		t.Fatalf("violation = %#v", got)
	}
	if !strings.Contains(got.Message, `"node:fs"`) {
		t.Fatalf("message = %q", got.Message)
	}
}

func TestBrowserSecretReaderIsBanned(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/main.ts": "import 'dotenv/config'\n",
	})

	violations, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || violations[0].Rule != "browser-secret-reader" || violations[0].Specifier != "dotenv/config" {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestBrowserSecretEnvIsBanned(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/main.ts": "const token = process.env.KATA_AUTH_TOKEN\n",
	})

	violations, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || violations[0].Rule != "browser-secret-env" {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestImportMetaEnvIsBanned(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/main.ts": "const mode = import.meta.env.MODE\n",
	})

	violations, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || violations[0].Rule != "browser-secret-env" {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestBrowserHostProcessIsBanned(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/main.ts": "const platform = process.platform\n",
	})

	violations, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || violations[0].Rule != "browser-host-process" {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCommentAndStringDoNotBan(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/main.ts": "" +
			"// import \"node:fs\" stays a comment\n" +
			"const note = 'process.env.KATA_AUTH_TOKEN'\n" +
			"import { onMount } from 'svelte'\n",
	})

	violations, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestBrowserPlaneEscapeIsBanned(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/main.ts":    "import { start } from '../scripts/dev'\n",
		"web/scripts/dev.ts": "export function start() {}\n",
	})

	violations, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || violations[0].Rule != "browser-plane-escape" {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestBrowserHostModuleIsBanned(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/main.ts":                "import { devDaemonCommand } from './lib/dev-environment'\n",
		"web/src/lib/dev-environment.ts": "export function devDaemonCommand() { return process.platform }\n",
	})

	violations, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || violations[0].Rule != "browser-host-module" || violations[0].File != "web/src/main.ts" {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestParallelBoardClientIsBanned(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/main.ts":     "import { LinearClient } from '@linear/sdk'\n",
		"pkg/client/extra.go": "package client\n\nimport _ \"github.com/andygrunwald/go-jira\"\n",
	})

	violations, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 2 {
		t.Fatalf("violations = %#v", violations)
	}
	for _, got := range violations {
		if got.Rule != "parallel-board-client" {
			t.Fatalf("violation = %#v", got)
		}
	}
}

func TestUncredentialedAPIClientIsBanned(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/lib/api/client.ts": "import createClient from 'openapi-fetch'\n",
		"web/src/main.ts":           "import createClient from 'openapi-fetch'\n",
	})

	violations, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || violations[0].File != "web/src/main.ts" || violations[0].Rule != "browser-uncredentialed-api-client" {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestUnknownBrowserPackageIsDenied(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/main.ts": "import leftPad from 'left-pad'\n",
	})

	violations, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || violations[0].Rule != "browser-package-denied" || violations[0].Specifier != "left-pad" {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestPublicPackageCannotImportPrivilegedInternal(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/main.ts":        "import { onMount } from 'svelte'\n",
		"pkg/client/shortcut.go": "package client\n\nimport _ \"go.kenn.io/kata/internal/daemon\"\n",
	})

	violations, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || violations[0].Rule != "public-pkg-privileged-internal" || violations[0].Specifier != "go.kenn.io/kata/internal/daemon" {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestPublicPackageMayImportDeclaredClient(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/main.ts":      "import { onMount } from 'svelte'\n",
		"pkg/client/client.go": "package client\n\nimport _ \"go.kenn.io/kata/internal/client\"\n",
		"cmd/kata/main.go":     "package main\n\nimport _ \"go.kenn.io/kata/internal/daemon\"\n",
	})

	violations, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestSvelteUnclosedScriptFailsClosed(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/App.svelte": "<script lang=\"ts\">\nimport { onMount } from 'svelte'\n",
	})

	_, err := Check(root)
	if err == nil {
		t.Fatal("expected fail-closed parse error")
	}
}

func TestGoImportParseErrorFailsClosed(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/main.ts":   "import { onMount } from 'svelte'\n",
		"pkg/client/bad.go": "package client\n\nimport \"unterminated\n",
	})

	_, err := Check(root)
	if err == nil {
		t.Fatal("expected fail-closed parse error")
	}
}

func TestDynamicNonLiteralImportFailsClosed(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/main.ts": "const name = './client'\nconst mod = import(name)\n",
	})

	violations, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || violations[0].Rule != "browser-dynamic-import" {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestCleanFixturePasses(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/main.ts":                         "import App from './App.svelte'\nimport './app.css'\n",
		"web/src/App.svelte":                      "<script lang=\"ts\">\n  import { onMount } from 'svelte'\n  import { createKataClient } from './lib/api/client'\n</script>\n",
		"web/src/lib/api/client.ts":               "import createClient from 'openapi-fetch'\nexport function createKataClient() { return createClient }\n",
		"web/src/app.css":                         "body { color: black; }\n",
		"web/src/lib/dev-environment.ts":          "export const platform = process.platform\n",
		"packages/kata-ui/src/index.ts":           "export { IssueDetail } from './IssueDetail.svelte'\n",
		"packages/kata-ui/src/IssueDetail.svelte": "<script lang=\"ts\">\n  import { Button } from '@kenn-io/kit-ui'\n</script>\n",
		"pkg/client/client.go":                    "package client\n\nimport _ \"go.kenn.io/kata/internal/client\"\n",
	})

	violations, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestMissingBrowserPlaneFailsClosed(t *testing.T) {
	root := t.TempDir()
	_, err := Check(root)
	if err == nil {
		t.Fatal("expected fail-closed error when web/src is missing")
	}
}

func TestRepositoryImportBans(t *testing.T) {
	root := repositoryRoot(t)
	violations, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range violations {
		t.Errorf("%s:%d [%s] %s", got.File, got.Line, got.Rule, got.Message)
	}
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}
