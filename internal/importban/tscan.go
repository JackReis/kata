package importban

import (
	"fmt"
	"strings"
)

type tsEdge struct {
	line      int
	specifier string
	rule      string
}

type scriptChunk struct {
	text string
	line int
}

type lexer struct {
	src   string
	i     int
	line  int
	edges []tsEdge
}

func scanTypeScript(src string, lineBase int) ([]tsEdge, error) {
	l := &lexer{src: src, line: lineBase}
	if err := l.scanCode(); err != nil {
		return nil, err
	}
	return l.edges, nil
}

func (l *lexer) scanCode() error {
	for l.i < len(l.src) {
		l.skipSpace()
		if l.i >= len(l.src) {
			return nil
		}
		switch {
		case l.atKeyword("import"):
			if l.propertyKeyFollows("import") {
				l.consumeKeyword("import")
				continue
			}
			if err := l.parseImport(); err != nil {
				return err
			}
		case l.atKeyword("export"):
			if l.propertyKeyFollows("export") {
				l.consumeKeyword("export")
				continue
			}
			if err := l.parseExport(); err != nil {
				return err
			}
		case l.atKeyword("process"):
			if l.propertyKeyFollows("process") {
				l.consumeKeyword("process")
				continue
			}
			line := l.line
			l.consumeKeyword("process")
			rule := "browser-host-process"
			if l.hasPrefix(".env") && !identContinue(l.src, l.i+len(".env")) {
				rule = "browser-secret-env"
			}
			l.edges = append(l.edges, tsEdge{line: line, rule: rule})
		case l.atKeyword("Bun"):
			line := l.line
			l.consumeKeyword("Bun")
			if l.hasPrefix(".env") && !identContinue(l.src, l.i+len(".env")) {
				l.edges = append(l.edges, tsEdge{line: line, rule: "browser-secret-env", specifier: "Bun.env"})
			}
		case l.src[l.i] == '\'' || l.src[l.i] == '"' || l.src[l.i] == '`':
			if err := l.skipString(); err != nil {
				return err
			}
		default:
			l.advance()
		}
	}
	return nil
}

func (l *lexer) parseImport() error {
	line := l.line
	l.consumeKeyword("import")
	l.skipSpace()
	if l.hasPrefix(".") {
		if !l.consume(".meta") {
			return fmt.Errorf("line %d: unrecognized import.", line)
		}
		if l.hasPrefix(".env") && !identContinue(l.src, l.i+len(".env")) {
			l.edges = append(l.edges, tsEdge{line: line, rule: "browser-secret-env", specifier: "import.meta.env"})
		}
		return nil
	}
	if l.hasPrefix("(") {
		l.advance()
		l.skipSpace()
		if l.hasPrefix("'") || l.hasPrefix("\"") {
			spec, specLine, err := l.readString()
			if err != nil {
				return err
			}
			l.edges = append(l.edges, tsEdge{line: specLine, specifier: spec})
			l.skipSpace()
			if !l.hasPrefix(")") {
				return fmt.Errorf("line %d: import() missing closing parenthesis", l.line)
			}
			l.advance()
			return nil
		}
		l.edges = append(l.edges, tsEdge{line: line, rule: "browser-dynamic-import"})
		return l.skipBalanced('(', ')')
	}
	if l.hasPrefix("'") || l.hasPrefix("\"") {
		spec, specLine, err := l.readString()
		if err != nil {
			return err
		}
		l.edges = append(l.edges, tsEdge{line: specLine, specifier: spec})
		return nil
	}
	if l.atKeyword("type") {
		l.consumeKeyword("type")
		l.skipSpace()
	}
	switch {
	case l.hasPrefix("{"):
		if err := l.skipGroup('{', '}'); err != nil {
			return err
		}
	case l.hasPrefix("*"):
		if err := l.skipStarAlias(); err != nil {
			return err
		}
	case l.atIdent():
		l.readIdent()
		l.skipSpace()
		if l.hasPrefix(",") {
			l.advance()
			l.skipSpace()
			switch {
			case l.hasPrefix("{"):
				if err := l.skipGroup('{', '}'); err != nil {
					return err
				}
			case l.hasPrefix("*"):
				if err := l.skipStarAlias(); err != nil {
					return err
				}
			default:
				return fmt.Errorf("line %d: unrecognized import clause", l.line)
			}
		}
	default:
		return fmt.Errorf("line %d: unrecognized import", l.line)
	}
	l.skipSpace()
	if !l.consumeKeyword("from") {
		return fmt.Errorf("line %d: import missing from", l.line)
	}
	l.skipSpace()
	spec, specLine, err := l.readString()
	if err != nil {
		return err
	}
	l.edges = append(l.edges, tsEdge{line: specLine, specifier: spec})
	return nil
}

func (l *lexer) parseExport() error {
	l.consumeKeyword("export")
	l.skipSpace()
	if l.atKeyword("type") {
		l.consumeKeyword("type")
		l.skipSpace()
		if !l.hasPrefix("{") {
			return nil
		}
	}
	switch {
	case l.hasPrefix("{"):
		if err := l.skipGroup('{', '}'); err != nil {
			return err
		}
		l.skipSpace()
		if !l.consumeKeyword("from") {
			return nil
		}
		l.skipSpace()
		spec, specLine, err := l.readString()
		if err != nil {
			return err
		}
		l.edges = append(l.edges, tsEdge{line: specLine, specifier: spec})
		return nil
	case l.hasPrefix("*"):
		l.advance()
		l.skipSpace()
		if l.consumeKeyword("as") {
			l.skipSpace()
			if !l.atIdent() {
				return fmt.Errorf("line %d: export * as missing name", l.line)
			}
			l.readIdent()
			l.skipSpace()
		}
		if !l.consumeKeyword("from") {
			return fmt.Errorf("line %d: export * missing from", l.line)
		}
		l.skipSpace()
		spec, specLine, err := l.readString()
		if err != nil {
			return err
		}
		l.edges = append(l.edges, tsEdge{line: specLine, specifier: spec})
		return nil
	default:
		return nil
	}
}

func (l *lexer) skipStarAlias() error {
	if !l.hasPrefix("*") {
		return fmt.Errorf("line %d: expected *", l.line)
	}
	l.advance()
	l.skipSpace()
	if !l.consumeKeyword("as") {
		return fmt.Errorf("line %d: import * missing as", l.line)
	}
	l.skipSpace()
	if !l.atIdent() {
		return fmt.Errorf("line %d: import * as missing name", l.line)
	}
	l.readIdent()
	return nil
}

func (l *lexer) skipGroup(open, close byte) error {
	if !l.hasPrefix(string(open)) {
		return fmt.Errorf("line %d: expected %c", l.line, open)
	}
	l.advance()
	return l.skipBalanced(open, close)
}

func (l *lexer) skipBalanced(open, close byte) error {
	depth := 1
	for l.i < len(l.src) && depth > 0 {
		l.skipSpace()
		if l.i >= len(l.src) {
			break
		}
		switch l.src[l.i] {
		case '\'', '"', '`':
			if err := l.skipString(); err != nil {
				return err
			}
		default:
			if l.src[l.i] == open {
				depth++
			} else if l.src[l.i] == close {
				depth--
			}
			l.advance()
		}
	}
	if depth != 0 {
		return fmt.Errorf("line %d: unclosed %c", l.line, open)
	}
	return nil
}

func (l *lexer) skipSpace() {
	for l.i < len(l.src) {
		switch l.src[l.i] {
		case ' ', '\t', '\r', '\n':
			l.advance()
		default:
			if strings.HasPrefix(l.src[l.i:], "//") {
				for l.i < len(l.src) && l.src[l.i] != '\n' {
					l.advance()
				}
				continue
			}
			if strings.HasPrefix(l.src[l.i:], "/*") {
				l.advance()
				l.advance()
				for l.i+1 < len(l.src) && !(l.src[l.i] == '*' && l.src[l.i+1] == '/') {
					l.advance()
				}
				if l.i+1 < len(l.src) {
					l.advance()
					l.advance()
				}
				continue
			}
			return
		}
	}
}

func (l *lexer) skipString() error {
	if l.i >= len(l.src) {
		return fmt.Errorf("line %d: unclosed string", l.line)
	}
	quote := l.src[l.i]
	l.advance()
	if quote == '`' {
		return l.skipTemplate()
	}
	for l.i < len(l.src) {
		switch l.src[l.i] {
		case '\\':
			l.advance()
			if l.i < len(l.src) {
				l.advance()
			}
		case '\n':
			return fmt.Errorf("line %d: unclosed string", l.line)
		default:
			if l.src[l.i] == quote {
				l.advance()
				return nil
			}
			l.advance()
		}
	}
	return fmt.Errorf("line %d: unclosed string", l.line)
}

func (l *lexer) skipTemplate() error {
	for l.i < len(l.src) {
		switch l.src[l.i] {
		case '\\':
			l.advance()
			if l.i < len(l.src) {
				l.advance()
			}
		case '`':
			l.advance()
			return nil
		case '$':
			if l.i+1 < len(l.src) && l.src[l.i+1] == '{' {
				l.advance()
				l.advance()
				start := l.i
				startLine := l.line
				if err := l.skipBalanced('{', '}'); err != nil {
					return err
				}
				end := l.i - 1
				if end < start {
					end = start
				}
				nested, err := scanTypeScript(l.src[start:end], startLine)
				if err != nil {
					return err
				}
				l.edges = append(l.edges, nested...)
				continue
			}
			l.advance()
		default:
			l.advance()
		}
	}
	return fmt.Errorf("line %d: unclosed template", l.line)
}

func (l *lexer) readString() (string, int, error) {
	if l.i >= len(l.src) || (l.src[l.i] != '\'' && l.src[l.i] != '"') {
		return "", 0, fmt.Errorf("line %d: expected module specifier string", l.line)
	}
	quote := l.src[l.i]
	line := l.line
	l.advance()
	var b strings.Builder
	for l.i < len(l.src) {
		switch l.src[l.i] {
		case '\\':
			l.advance()
			if l.i >= len(l.src) {
				return "", 0, fmt.Errorf("line %d: unclosed string", line)
			}
			b.WriteByte(l.src[l.i])
			l.advance()
		case '\n':
			return "", 0, fmt.Errorf("line %d: unclosed string", line)
		default:
			if l.src[l.i] == quote {
				l.advance()
				return b.String(), line, nil
			}
			b.WriteByte(l.src[l.i])
			l.advance()
		}
	}
	return "", 0, fmt.Errorf("line %d: unclosed string", line)
}

func (l *lexer) propertyKeyFollows(word string) bool {
	i := l.i + len(word)
	for i < len(l.src) {
		switch l.src[i] {
		case ' ', '\t', '\r', '\n':
			i++
		default:
			if strings.HasPrefix(l.src[i:], "//") || strings.HasPrefix(l.src[i:], "/*") {
				return false
			}
			if strings.HasPrefix(l.src[i:], "?:") {
				return true
			}
			return l.src[i] == ':'
		}
	}
	return false
}

func (l *lexer) atKeyword(word string) bool {
	if !strings.HasPrefix(l.src[l.i:], word) {
		return false
	}
	if identContinue(l.src, l.i+len(word)) {
		return false
	}
	if l.i > 0 && isIdentByte(l.src[l.i-1]) {
		return false
	}
	return true
}

func (l *lexer) consumeKeyword(word string) bool {
	if !l.atKeyword(word) {
		return false
	}
	l.i += len(word)
	return true
}

func (l *lexer) consume(s string) bool {
	if !strings.HasPrefix(l.src[l.i:], s) {
		return false
	}
	for range len(s) {
		l.advance()
	}
	return true
}

func (l *lexer) hasPrefix(s string) bool {
	return strings.HasPrefix(l.src[l.i:], s)
}

func (l *lexer) atIdent() bool {
	return l.i < len(l.src) && isIdentStart(l.src[l.i])
}

func (l *lexer) readIdent() {
	for l.i < len(l.src) && isIdentByte(l.src[l.i]) {
		l.advance()
	}
}

func (l *lexer) advance() {
	if l.i >= len(l.src) {
		return
	}
	if l.src[l.i] == '\n' {
		l.line++
	}
	l.i++
}

func identContinue(src string, i int) bool {
	return i < len(src) && isIdentByte(src[i])
}

func isIdentStart(b byte) bool {
	return b == '_' || b == '$' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func isIdentByte(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

func extractScripts(src string) ([]scriptChunk, string, error) {
	var chunks []scriptChunk
	masked := []byte(src)
	i := 0
	line := 1
	for i < len(src) {
		if strings.HasPrefix(strings.ToLower(src[i:]), "<script") && !identContinue(src, i+len("<script")) {
			tagLine := line
			j := i + len("<script")
			inQuote := byte(0)
			for j < len(src) {
				if src[j] == '\n' {
					line++
				}
				if inQuote != 0 {
					if src[j] == inQuote {
						inQuote = 0
					}
					j++
					continue
				}
				if src[j] == '\'' || src[j] == '"' {
					inQuote = src[j]
					j++
					continue
				}
				if src[j] == '>' {
					break
				}
				j++
			}
			if j >= len(src) || src[j] != '>' {
				return nil, "", fmt.Errorf("line %d: unclosed script tag", tagLine)
			}
			j++
			contentLine := line
			rel := strings.Index(strings.ToLower(src[j:]), "</script>")
			if rel < 0 {
				return nil, "", fmt.Errorf("line %d: unclosed script", contentLine)
			}
			chunks = append(chunks, scriptChunk{text: src[j : j+rel], line: contentLine})
			for k := j; k < j+rel; k++ {
				if masked[k] != '\n' {
					masked[k] = ' '
				}
			}
			end := j + rel + len("</script>")
			for k := j; k < end && k < len(src); k++ {
				if src[k] == '\n' {
					line++
				}
			}
			i = end
			continue
		}
		if strings.HasPrefix(strings.ToLower(src[i:]), "<style") && !identContinue(src, i+len("<style")) {
			j := i + len("<style")
			inQuote := byte(0)
			for j < len(src) {
				if src[j] == '\n' {
					line++
				}
				if inQuote != 0 {
					if src[j] == inQuote {
						inQuote = 0
					}
					j++
					continue
				}
				if src[j] == '\'' || src[j] == '"' {
					inQuote = src[j]
					j++
					continue
				}
				if src[j] == '>' {
					break
				}
				j++
			}
			if j >= len(src) || src[j] != '>' {
				return nil, "", fmt.Errorf("line %d: unclosed style tag", line)
			}
			j++
			rel := strings.Index(strings.ToLower(src[j:]), "</style>")
			if rel < 0 {
				return nil, "", fmt.Errorf("line %d: unclosed style", line)
			}
			end := j + rel + len("</style>")
			for k := i; k < end && k < len(masked); k++ {
				if masked[k] != '\n' {
					masked[k] = ' '
				}
			}
			for k := j; k < end && k < len(src); k++ {
				if src[k] == '\n' {
					line++
				}
			}
			i = end
			continue
		}
		if src[i] == '\n' {
			line++
		}
		i++
	}
	return chunks, string(masked), nil
}

func scanMustaches(masked string) ([]tsEdge, error) {
	var edges []tsEdge
	line := 1
	for i := 0; i < len(masked); {
		if masked[i] == '\n' {
			line++
			i++
			continue
		}
		if masked[i] != '{' {
			i++
			continue
		}
		startLine := line
		j := i + 1
		depth := 1
		k := j
		kLine := line
		for k < len(masked) && depth > 0 {
			if masked[k] == '\n' {
				kLine++
			}
			switch masked[k] {
			case '{':
				depth++
				k++
			case '}':
				depth--
				k++
			case '\'', '"', '`':
				end, endLine, err := skipQuoted(masked, k, kLine)
				if err != nil {
					return nil, err
				}
				k, kLine = end, endLine
			default:
				k++
			}
		}
		if depth != 0 {
			return nil, fmt.Errorf("line %d: unclosed mustache", startLine)
		}
		inner := masked[j : k-1]
		if strings.Contains(inner, "import") || strings.Contains(inner, "process") || strings.Contains(inner, "Bun") {
			nested, err := scanTypeScript(inner, startLine)
			if err != nil {
				return nil, err
			}
			edges = append(edges, nested...)
		}
		line = kLine
		i = k
	}
	return edges, nil
}

func skipQuoted(src string, i, line int) (int, int, error) {
	quote := src[i]
	i++
	for i < len(src) {
		if src[i] == '\n' {
			line++
		}
		if src[i] == '\\' {
			i++
			if i < len(src) {
				if src[i] == '\n' {
					line++
				}
				i++
			}
			continue
		}
		if src[i] == quote {
			return i + 1, line, nil
		}
		i++
	}
	return 0, 0, fmt.Errorf("line %d: unclosed string", line)
}

func browserEdges(rel, src string) ([]tsEdge, error) {
	if strings.HasSuffix(rel, ".svelte") {
		chunks, masked, err := extractScripts(src)
		if err != nil {
			return nil, err
		}
		var edges []tsEdge
		for _, chunk := range chunks {
			found, err := scanTypeScript(chunk.text, chunk.line)
			if err != nil {
				return nil, err
			}
			edges = append(edges, found...)
		}
		mustache, err := scanMustaches(masked)
		if err != nil {
			return nil, err
		}
		edges = append(edges, mustache...)
		return edges, nil
	}
	return scanTypeScript(src, 1)
}
