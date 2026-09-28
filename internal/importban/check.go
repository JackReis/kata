package importban

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Violation is one banned import-graph edge.
type Violation struct {
	File      string
	Line      int
	Rule      string
	Specifier string
	Message   string
}

const (
	hostModule = "web/src/lib/dev-environment.ts"
	apiClient  = "web/src/lib/api/client.ts"
	webPlane   = "web/src"
	uiPlane    = "packages/kata-ui/src"
)

// Check walks root and returns banned edges.
// A parse failure, an unresolved relative import, or a missing browser plane
// returns an error so the gate fails closed instead of skipping the file.
// File reads go through os so go test records them as cache inputs.
func Check(root string) ([]Violation, error) {
	info, err := os.Stat(filepath.Join(root, "web", "src"))
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("import bans: browser plane web/src missing")
	}

	var violations []Violation
	browserFiles := 0
	walkErr := filepath.WalkDir(root, func(abs string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "node_modules", ".git", "dist", "playwright-report", "test-results":
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case strings.HasSuffix(rel, ".go"):
			found, err := checkGoFile(abs, rel)
			if err != nil {
				return err
			}
			violations = append(violations, found...)
		case isBrowserSource(rel):
			browserFiles++
			found, err := checkBrowserFile(root, abs, rel)
			if err != nil {
				return fmt.Errorf("import bans: %s: %w", rel, err)
			}
			violations = append(violations, found...)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	if browserFiles == 0 {
		return nil, fmt.Errorf("import bans: scanned zero browser files")
	}
	slices.SortFunc(violations, func(a, b Violation) int {
		if a.File != b.File {
			return strings.Compare(a.File, b.File)
		}
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		if a.Rule != b.Rule {
			return strings.Compare(a.Rule, b.Rule)
		}
		return strings.Compare(a.Specifier, b.Specifier)
	})
	return violations, nil
}

func isBrowserSource(rel string) bool {
	if rel == hostModule || isTestSource(rel) || strings.Contains(rel, "/__fixtures__/") {
		return false
	}
	if !strings.HasPrefix(rel, webPlane+"/") && !strings.HasPrefix(rel, uiPlane+"/") {
		return false
	}
	return strings.HasSuffix(rel, ".ts") || strings.HasSuffix(rel, ".svelte") || strings.HasSuffix(rel, ".js")
}

func isTestSource(rel string) bool {
	base := path.Base(rel)
	return strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".test.js") ||
		strings.HasSuffix(base, ".spec.ts") ||
		strings.HasSuffix(base, ".spec.js")
}

func checkBrowserFile(root, abs, rel string) ([]Violation, error) {
	body, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	edges, err := browserEdges(rel, string(body))
	if err != nil {
		return nil, err
	}
	var violations []Violation
	for _, edge := range edges {
		if edge.rule != "" && edge.specifier == "" {
			violations = append(violations, Violation{
				File:    rel,
				Line:    edge.line,
				Rule:    edge.rule,
				Message: hostEdgeMessage(edge.rule),
			})
			continue
		}
		if edge.rule == "browser-secret-env" && edge.specifier != "" {
			violations = append(violations, Violation{
				File:      rel,
				Line:      edge.line,
				Rule:      edge.rule,
				Specifier: edge.specifier,
				Message:   fmt.Sprintf("banned secret read %s on the browser plane", edge.specifier),
			})
			continue
		}
		found, err := classifySpecifier(root, rel, edge)
		if err != nil {
			return nil, err
		}
		if found != nil {
			violations = append(violations, *found)
		}
	}
	return violations, nil
}

func hostEdgeMessage(rule string) string {
	switch rule {
	case "browser-secret-env":
		return "banned process.env read on the browser plane"
	case "browser-host-process":
		return "banned host process read on the browser plane"
	case "browser-dynamic-import":
		return "banned non-literal import() on the browser plane"
	default:
		return "banned browser edge " + rule
	}
}

func classifySpecifier(root, rel string, edge tsEdge) (*Violation, error) {
	spec := edge.specifier
	if spec == "" {
		return nil, fmt.Errorf("line %d: empty module specifier", edge.line)
	}
	if strings.HasPrefix(spec, ".") {
		target, escaped, err := resolveRelative(root, rel, spec)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", edge.line, err)
		}
		if target == hostModule {
			return &Violation{
				File:      rel,
				Line:      edge.line,
				Rule:      "browser-host-module",
				Specifier: spec,
				Message:   fmt.Sprintf("banned import %q reaches host module %s", spec, hostModule),
			}, nil
		}
		plane := planeOf(rel)
		if escaped || !withinPlane(target, plane) {
			rule := "browser-plane-escape"
			if plane == uiPlane {
				rule = "ui-kit-plane-escape"
			}
			return &Violation{
				File:      rel,
				Line:      edge.line,
				Rule:      rule,
				Specifier: spec,
				Message:   fmt.Sprintf("banned import %q leaves the %s plane", spec, plane),
			}, nil
		}
		return nil, nil
	}
	if violation, ok := bannedBareSpecifier(rel, spec, edge.line); ok {
		return &violation, nil
	}
	if allowedBare(rel, spec) {
		return nil, nil
	}
	rule := "browser-package-denied"
	if strings.HasPrefix(rel, uiPlane+"/") {
		rule = "ui-kit-package-denied"
	}
	return &Violation{
		File:      rel,
		Line:      edge.line,
		Rule:      rule,
		Specifier: spec,
		Message:   fmt.Sprintf("banned import %q is not on the browser allowlist", spec),
	}, nil
}

func planeOf(rel string) string {
	if strings.HasPrefix(rel, uiPlane+"/") {
		return uiPlane
	}
	return webPlane
}

func withinPlane(rel, plane string) bool {
	return rel == plane || strings.HasPrefix(rel, plane+"/")
}

func bannedBareSpecifier(rel, spec string, line int) (Violation, bool) {
	base := Violation{File: rel, Line: line, Specifier: spec}
	if isParallelBoard(spec) {
		base.Rule = "parallel-board-client"
		base.Message = fmt.Sprintf("banned import %q is a parallel assignment client; Multica is the one assignment plane", spec)
		return base, true
	}
	if isNodeBuiltin(spec) {
		base.Rule = "browser-node-builtin"
		base.Message = fmt.Sprintf("banned import %q on the browser plane (privileged host module)", spec)
		return base, true
	}
	if matchesModule(spec, "dotenv") {
		base.Rule = "browser-secret-reader"
		base.Message = fmt.Sprintf("banned import %q reads environment secrets into the browser plane", spec)
		return base, true
	}
	if matchesModule(spec, "openapi-fetch") && rel != apiClient {
		base.Rule = "browser-uncredentialed-api-client"
		base.Message = fmt.Sprintf("banned import %q skips the credentialed API client %s", spec, apiClient)
		return base, true
	}
	return Violation{}, false
}

func allowedBare(rel, spec string) bool {
	if matchesModule(spec, "openapi-fetch") && rel == apiClient {
		return true
	}
	allow := []string{
		"svelte",
		"@kenn-io/kit-ui",
		"@lucide/svelte",
		"@xyflow/svelte",
		"elkjs",
	}
	if strings.HasPrefix(rel, webPlane+"/") {
		allow = append(allow, "@kenn-io/kata-ui")
	}
	for _, prefix := range allow {
		if matchesModule(spec, prefix) {
			return true
		}
	}
	return false
}

func isNodeBuiltin(spec string) bool {
	if strings.HasPrefix(spec, "node:") {
		return true
	}
	base := spec
	if slash := strings.IndexByte(base, '/'); slash >= 0 {
		base = base[:slash]
	}
	switch base {
	case "fs", "child_process", "os", "path", "net", "http", "https", "tls", "dns", "crypto",
		"worker_threads", "vm", "module", "process", "buffer", "stream", "zlib", "readline",
		"cluster", "dgram", "perf_hooks", "inspector", "async_hooks", "v8", "url", "util":
		return true
	default:
		return false
	}
}

func isParallelBoard(spec string) bool {
	banned := []string{
		"@linear/",
		"@multica/",
		"multica",
		"linear",
		"jira.js",
		"jira-client",
		"@atlassian/jira",
		"asana",
		"trello",
		"github.com/andygrunwald/go-jira",
		"github.com/ctreminiom/go-atlassian",
		"github.com/linear/",
		"github.com/multica/",
	}
	for _, item := range banned {
		if matchesModule(spec, item) {
			return true
		}
	}
	return false
}

func matchesModule(spec, banned string) bool {
	if strings.HasSuffix(banned, "/") {
		return strings.HasPrefix(spec, banned)
	}
	return spec == banned || strings.HasPrefix(spec, banned+"/")
}

func resolveRelative(root, fromRel, spec string) (string, bool, error) {
	joined := path.Clean(path.Join(path.Dir(fromRel), spec))
	if joined == ".." || strings.HasPrefix(joined, "../") || path.IsAbs(joined) {
		return joined, true, nil
	}
	for _, candidate := range relativeCandidates(spec, joined) {
		if candidate == ".." || strings.HasPrefix(candidate, "../") || path.IsAbs(candidate) {
			return candidate, true, nil
		}
		abs := filepath.Join(root, filepath.FromSlash(candidate))
		info, err := os.Stat(abs)
		if err == nil && !info.IsDir() {
			return candidate, false, nil
		}
	}
	if !withinPlane(joined, planeOf(fromRel)) {
		return joined, true, nil
	}
	return "", false, fmt.Errorf("unresolved import %q from %s", spec, fromRel)
}

func relativeCandidates(spec, joined string) []string {
	var candidates []string
	if strings.HasSuffix(spec, ".js") {
		stem := strings.TrimSuffix(joined, ".js")
		candidates = append(candidates, stem+".ts", stem+".d.ts", stem+".tsx", stem+".svelte", stem+".svelte.ts")
	}
	candidates = append(candidates, joined)
	if knownAssetExt(joined) {
		return candidates
	}
	candidates = append(candidates,
		joined+".ts",
		joined+".tsx",
		joined+".d.ts",
		joined+".svelte",
		joined+".svelte.ts",
		joined+".js",
		joined+".mjs",
		path.Join(joined, "index.ts"),
		path.Join(joined, "index.d.ts"),
		path.Join(joined, "index.js"),
	)
	return candidates
}

func knownAssetExt(name string) bool {
	switch path.Ext(name) {
	case ".css", ".svg", ".png", ".webp", ".gif", ".jpg", ".jpeg", ".woff", ".woff2", ".json":
		return true
	default:
		return false
	}
}

func checkGoFile(abs, rel string) ([]Violation, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, abs, nil, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("import bans: %s: %w", rel, err)
	}
	var violations []Violation
	for _, imp := range file.Imports {
		spec, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("import bans: %s: %w", rel, err)
		}
		line := fset.Position(imp.Path.Pos()).Line
		if isParallelBoard(spec) {
			violations = append(violations, Violation{
				File:      rel,
				Line:      line,
				Rule:      "parallel-board-client",
				Specifier: spec,
				Message:   fmt.Sprintf("banned import %q is a parallel assignment client; Multica is the one assignment plane", spec),
			})
			continue
		}
		if !strings.HasPrefix(rel, "pkg/") || !isPrivilegedInternal(spec) {
			continue
		}
		violations = append(violations, Violation{
			File:      rel,
			Line:      line,
			Rule:      "public-pkg-privileged-internal",
			Specifier: spec,
			Message:   fmt.Sprintf("banned import %q from public package %s; use the declared HTTP API client", spec, rel),
		})
	}
	return violations, nil
}

func isPrivilegedInternal(spec string) bool {
	const internalRoot = "go.kenn.io/kata/internal"
	if spec != internalRoot && !strings.HasPrefix(spec, internalRoot+"/") {
		return false
	}
	// Declared exceptions. internal/client is the HTTP transport behind
	// pkg/client. identityaudit is the conformance kit's current internal
	// reach; a third internal import is rejected.
	allowed := []string{
		"go.kenn.io/kata/internal/client",
		"go.kenn.io/kata/internal/connector/identityaudit",
	}
	for _, item := range allowed {
		if matchesModule(spec, item) {
			return false
		}
	}
	return true
}
