// Command duneimport fails closed when a production import crosses a banned edge.
//
// Exit 0 means the scanned graph satisfies the policy. Exit 1 means at least
// one banned edge is present. Exit 2 means the gate itself is unsound: the
// policy is missing or incomplete, a scope produced no production files, or a
// rule does not match any scanned file.
package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	exitOK         = 0
	exitBanned     = 1
	exitFailClosed = 2
)

func main() {
	root := flag.String("root", ".", "module root to scan")
	policy := flag.String("policy", "", "policy JSON (default: <root>/tools/duneimport/policy.json)")
	flag.Parse()
	os.Exit(run(os.Stderr, *root, *policy))
}

func run(stderr io.Writer, root, policyPath string) int {
	if policyPath == "" {
		policyPath = filepath.Join(root, "tools", "duneimport", "policy.json")
	}
	pol, err := loadPolicy(policyPath)
	if err != nil {
		writeFail(stderr, err.Error())
		return exitFailClosed
	}
	files, err := scan(root, pol)
	if err != nil {
		writeFail(stderr, err.Error())
		return exitFailClosed
	}
	if msgs := deadRules(pol, files); len(msgs) > 0 {
		writeFail(stderr, strings.Join(msgs, "\n"))
		return exitFailClosed
	}
	violations := match(pol, files)
	if len(violations) == 0 {
		return exitOK
	}
	slices.SortFunc(violations, func(a, b violation) int {
		if c := cmp.Compare(a.File, b.File); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Line, b.Line); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Rule, b.Rule); c != 0 {
			return c
		}
		return cmp.Compare(a.Import, b.Import)
	})
	writef(stderr, "dune import gate: %d banned edge(s)\n", len(violations))
	for _, v := range violations {
		writef(stderr, "%s:%d: import '%s'\n  rule %s: %s\n", v.File, v.Line, v.Import, v.Rule, v.Reason)
	}
	return exitBanned
}

func writeFail(stderr io.Writer, message string) {
	writef(stderr, "dune import gate: FAIL-closed\n%s\n", message)
}

func writef(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

type policy struct {
	Version    int     `json:"version"`
	FailClosed *bool   `json:"fail_closed"`
	Scopes     []scope `json:"scopes"`
	Denies     []deny  `json:"denies"`
}

type scope struct {
	ID       string `json:"id"`
	Language string `json:"language"`
	Root     string `json:"root"`
}

type deny struct {
	ID              string   `json:"id"`
	Scope           string   `json:"scope"`
	From            string   `json:"from"`
	To              []string `json:"to"`
	ExceptFrom      []string `json:"except_from,omitempty"`
	ExceptFromExact []string `json:"except_from_exact,omitempty"`
	Reason          string   `json:"reason"`
}

type scannedFile struct {
	scope    string
	rel      string
	identity string
	imports  []imported
}

type imported struct {
	path string
	line int
}

type violation struct {
	File   string
	Line   int
	Import string
	Rule   string
	Reason string
}

func loadPolicy(path string) (policy, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return policy{}, fmt.Errorf("policy file %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var pol policy
	if err := dec.Decode(&pol); err != nil {
		return policy{}, fmt.Errorf("policy file %s: %w", path, err)
	}
	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		return policy{}, fmt.Errorf("policy file %s: extra JSON value", path)
	}
	if msgs := validatePolicy(pol); len(msgs) > 0 {
		return policy{}, fmt.Errorf("%s", strings.Join(msgs, "\n"))
	}
	return pol, nil
}

func validatePolicy(pol policy) []string {
	var msgs []string
	if pol.Version != 1 {
		msgs = append(msgs, "policy version must be 1")
	}
	if pol.FailClosed == nil || !*pol.FailClosed {
		msgs = append(msgs, "fail_closed must be true")
	}
	if len(pol.Scopes) == 0 {
		msgs = append(msgs, "scopes must be non-empty")
	}
	if len(pol.Denies) == 0 {
		msgs = append(msgs, "denies must be non-empty")
	}
	scopes := map[string]scope{}
	for _, sc := range pol.Scopes {
		if sc.ID == "" {
			msgs = append(msgs, "scope id must be non-empty")
			continue
		}
		if _, ok := scopes[sc.ID]; ok {
			msgs = append(msgs, "duplicate scope id "+sc.ID)
		}
		scopes[sc.ID] = sc
		switch sc.Language {
		case "go", "ts":
		default:
			msgs = append(msgs, "scope "+sc.ID+`: language must be "go" or "ts"`)
		}
		if sc.Root == "" {
			msgs = append(msgs, "scope "+sc.ID+": root must be non-empty")
			continue
		}
		if _, err := scopeDir(".", sc.Root); err != nil {
			msgs = append(msgs, "scope "+sc.ID+": "+err.Error())
		}
	}
	seenDeny := map[string]bool{}
	usedScope := map[string]bool{}
	for _, rule := range pol.Denies {
		if rule.ID == "" {
			msgs = append(msgs, "deny id must be non-empty")
		} else if seenDeny[rule.ID] {
			msgs = append(msgs, "duplicate deny id "+rule.ID)
		}
		seenDeny[rule.ID] = true
		if _, ok := scopes[rule.Scope]; !ok {
			msgs = append(msgs, "deny "+rule.ID+": unknown scope "+rule.Scope)
		}
		usedScope[rule.Scope] = true
		if rule.From == "" {
			msgs = append(msgs, "deny "+rule.ID+": from must be non-empty")
		}
		if len(rule.To) == 0 {
			msgs = append(msgs, "deny "+rule.ID+": to must be non-empty")
		}
		for _, to := range rule.To {
			if to == "" {
				msgs = append(msgs, "deny "+rule.ID+": to entry must be non-empty")
			}
		}
		for _, except := range rule.ExceptFrom {
			if except == "" {
				msgs = append(msgs, "deny "+rule.ID+": except_from entry must be non-empty")
			}
		}
		for _, except := range rule.ExceptFromExact {
			if except == "" {
				msgs = append(msgs, "deny "+rule.ID+": except_from_exact entry must be non-empty")
			}
		}
		if rule.Reason == "" {
			msgs = append(msgs, "deny "+rule.ID+": reason must be non-empty")
		}
	}
	for id := range scopes {
		if !usedScope[id] {
			msgs = append(msgs, "scope "+id+" has no denies")
		}
	}
	slices.Sort(msgs)
	return msgs
}

func scan(root string, pol policy) ([]scannedFile, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("module root %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("module root %s is not a directory", root)
	}
	var module string
	for _, sc := range pol.Scopes {
		if sc.Language == "go" {
			module, err = readModule(root)
			if err != nil {
				return nil, err
			}
			break
		}
	}
	var files []scannedFile
	var problems []string
	for _, sc := range pol.Scopes {
		dir, err := scopeDir(root, sc.Root)
		if err != nil {
			problems = append(problems, "scope "+sc.ID+": "+err.Error())
			continue
		}
		info, err := os.Stat(dir)
		if err != nil {
			problems = append(problems, fmt.Sprintf("scope %s: root %s: %v", sc.ID, sc.Root, err))
			continue
		}
		if !info.IsDir() {
			problems = append(problems, "scope "+sc.ID+": root "+sc.Root+" is not a directory")
			continue
		}
		found, err := walkScope(root, dir, sc, module)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if len(found) == 0 {
			label := "Go"
			if sc.Language == "ts" {
				label = "TS"
			}
			problems = append(problems, fmt.Sprintf("scope %s: no production %s files scanned under %s", sc.ID, label, sc.Root))
			continue
		}
		files = append(files, found...)
	}
	if len(problems) > 0 {
		slices.Sort(problems)
		return nil, fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	return files, nil
}

func readModule(root string) (string, error) {
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("go.mod: %w", err)
	}
	for line := range strings.SplitSeq(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("go.mod: module path missing")
}

func scopeDir(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("root %s must be relative", rel)
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("root %s escapes the module", rel)
	}
	return filepath.Join(root, clean), nil
}

func walkScope(root, dir string, sc scope, module string) ([]scannedFile, error) {
	var files []scannedFile
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != dir && skipDir(name) {
				return filepath.SkipDir
			}
			return nil
		}
		if skipDir(name) || !wantedFile(sc.Language, name) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var imports []imported
		switch sc.Language {
		case "go":
			imports, err = goImports(path, body)
		case "ts":
			imports = tsImports(rel, string(body))
		}
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		identity := rel
		if sc.Language == "go" {
			identity = goIdentity(module, rel)
		}
		files = append(files, scannedFile{
			scope:    sc.ID,
			rel:      rel,
			identity: identity,
			imports:  imports,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "site", ".cache", "coverage", "playwright-report", "test-results":
		return true
	default:
		return false
	}
}

func wantedFile(language, name string) bool {
	switch language {
	case "go":
		return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
	case "ts":
		switch filepath.Ext(name) {
		case ".ts", ".tsx", ".js", ".mjs", ".svelte", ".mts", ".cts":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func goIdentity(module, rel string) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." {
		return module
	}
	return module + "/" + dir
}

func goImports(path string, body []byte) ([]imported, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, body, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var imports []imported
	for _, spec := range file.Imports {
		if spec.Path == nil {
			continue
		}
		quoted, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, err
		}
		imports = append(imports, imported{
			path: quoted,
			line: fset.Position(spec.Path.Pos()).Line,
		})
	}
	return imports, nil
}

var (
	reFrom    = regexp.MustCompile(`\bfrom\s*['"]([^'"]+)['"]`)
	reImport  = regexp.MustCompile(`\bimport\s*['"]([^'"]+)['"]`)
	reDynamic = regexp.MustCompile(`\bimport\s*\(\s*['"]([^'"]+)['"]`)
)

func tsImports(rel, src string) []imported {
	spans := [][2]int{{0, len(src)}}
	if strings.HasSuffix(rel, ".svelte") {
		spans = scriptSpans(src)
	}
	seen := map[string]bool{}
	var imports []imported
	for _, re := range []*regexp.Regexp{reFrom, reImport, reDynamic} {
		for _, loc := range re.FindAllStringSubmatchIndex(src, -1) {
			if !inSpans(loc[0], spans) {
				continue
			}
			spec := src[loc[2]:loc[3]]
			line := 1 + strings.Count(src[:loc[0]], "\n")
			key := fmt.Sprintf("%d\x00%s", line, spec)
			if seen[key] {
				continue
			}
			seen[key] = true
			imports = append(imports, imported{path: spec, line: line})
		}
	}
	return imports
}

func scriptSpans(src string) [][2]int {
	var spans [][2]int
	offset := 0
	rest := src
	for {
		i := strings.Index(rest, "<script")
		if i < 0 {
			break
		}
		rest = rest[i:]
		offset += i
		gt := strings.Index(rest, ">")
		if gt < 0 {
			break
		}
		bodyStart := offset + gt + 1
		rest = rest[gt+1:]
		offset = bodyStart
		end := strings.Index(rest, "</script>")
		if end < 0 {
			break
		}
		spans = append(spans, [2]int{bodyStart, bodyStart + end})
		rest = rest[end+len("</script>"):]
		offset += end + len("</script>")
	}
	return spans
}

func inSpans(pos int, spans [][2]int) bool {
	for _, span := range spans {
		if pos >= span[0] && pos < span[1] {
			return true
		}
	}
	return false
}

func deadRules(pol policy, files []scannedFile) []string {
	var msgs []string
	for _, rule := range pol.Denies {
		if !identityHit(files, rule.Scope, rule.From, false) {
			msgs = append(msgs, fmt.Sprintf("rule %s from %s matched no scanned file", rule.ID, rule.From))
		}
		for _, except := range rule.ExceptFrom {
			if !identityHit(files, rule.Scope, except, false) {
				msgs = append(msgs, fmt.Sprintf("rule %s except_from %s matched no scanned file", rule.ID, except))
			}
		}
		for _, except := range rule.ExceptFromExact {
			if !identityHit(files, rule.Scope, except, true) {
				msgs = append(msgs, fmt.Sprintf("rule %s except_from_exact %s matched no scanned file", rule.ID, except))
			}
		}
	}
	slices.Sort(msgs)
	return msgs
}

func identityHit(files []scannedFile, scopeID, pattern string, exact bool) bool {
	for _, file := range files {
		if file.scope != scopeID {
			continue
		}
		if exact {
			if file.identity == pattern {
				return true
			}
			continue
		}
		if matchPattern(file.identity, pattern) {
			return true
		}
	}
	return false
}

func match(pol policy, files []scannedFile) []violation {
	var violations []violation
	for _, file := range files {
		for _, rule := range pol.Denies {
			if rule.Scope != file.scope || !matchPattern(file.identity, rule.From) {
				continue
			}
			if excepted(file.identity, rule) {
				continue
			}
			for _, imp := range file.imports {
				for _, to := range rule.To {
					if !matchPattern(imp.path, to) {
						continue
					}
					violations = append(violations, violation{
						File:   file.rel,
						Line:   imp.line,
						Import: imp.path,
						Rule:   rule.ID,
						Reason: rule.Reason,
					})
				}
			}
		}
	}
	return violations
}

func excepted(identity string, rule deny) bool {
	if slices.Contains(rule.ExceptFromExact, identity) {
		return true
	}
	for _, except := range rule.ExceptFrom {
		if matchPattern(identity, except) {
			return true
		}
	}
	return false
}

func matchPattern(value, pattern string) bool {
	if pattern == "" || value == "" {
		return false
	}
	if strings.HasSuffix(pattern, ":") {
		return strings.HasPrefix(value, pattern)
	}
	return value == pattern || strings.HasPrefix(value, pattern+"/")
}
