package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	contentSearchMaxFileBytes  = 4 << 20
	contentSearchMaxMatches    = 1000
	contentSearchMaxFileMatch  = 100
	contentSearchMaxLineRanges = 100
	contentSearchPreviewRunes  = 240
	contentSearchPreviewLeadIn = 20
)

// ContentQuery describes a workspace content search. Include and Exclude are
// comma separated globs relative to the workspace root: a leading "./" anchors
// a pattern at the root, other patterns match at any depth, and a pattern that
// names a folder also covers everything beneath it. SearchIgnored also visits
// Git-ignored files and common dependency directories.
type ContentQuery struct {
	Text                        string
	MatchCase, WholeWord, Regex bool
	Include, Exclude            string
	SearchIgnored               bool
}

// MatchRange is a half-open rune range of ContentMatch.Text.
type MatchRange struct{ Start, End int }

// ContentMatch is one matching line. Line and Col (1-based, in runes) locate
// the first match; Ranges mark every match shown in the Text preview.
type ContentMatch struct {
	Path      string
	Line, Col int
	Text      string
	Ranges    []MatchRange
}

// Count is the number of matches on the line.
func (m ContentMatch) Count() int { return max(1, len(m.Ranges)) }

type ContentSearchResult struct {
	Matches   []ContentMatch
	Truncated bool
}

// SearchContent scans regular text files under root for lines matching q.
// Binary files and unsupported encodings are skipped the same way the editor
// refuses them; oversized or unreadable files mark the result truncated.
func SearchContent(ctx context.Context, root string, q ContentQuery) (ContentSearchResult, error) {
	var result ContentSearchResult
	pattern, err := CompileContentQuery(q)
	if err != nil {
		return result, err
	}
	include, err := parseGlobs(q.Include)
	if err != nil {
		return result, err
	}
	exclude, err := parseGlobs(q.Exclude)
	if err != nil {
		return result, err
	}
	if matches, truncated, ok := searchContentRipgrep(ctx, root, q); ok {
		result.Matches = matches
		result.Truncated = truncated
		return result, nil
	}
	for _, dir := range include.roots(root) {
		truncated, err := walkEntries(ctx, root, dir, !q.SearchIgnored, func(v Entry) (bool, error) {
			rel := slashRel(root, v.Path)
			if exclude.match(rel) {
				return false, nil
			}
			if v.Dir {
				return true, nil
			}
			if len(include) > 0 && !include.match(rel) {
				return false, nil
			}
			if err := ctx.Err(); err != nil {
				return false, err
			}
			matches, incomplete := searchFileContent(v.Path, pattern, q.WholeWord, min(contentSearchMaxFileMatch, contentSearchMaxMatches-len(result.Matches)))
			result.Matches = append(result.Matches, matches...)
			if incomplete {
				result.Truncated = true
				if len(result.Matches) == contentSearchMaxMatches {
					return false, errSearchLimit
				}
			}
			return false, nil
		})
		result.Truncated = result.Truncated || truncated
		if err != nil || truncated {
			return result, err
		}
	}
	return result, nil
}

// CompileContentQuery compiles the text pattern of q the same way SearchContent
// does. The file viewer reuses it to highlight the searched keywords.
func CompileContentQuery(q ContentQuery) (*regexp.Regexp, error) {
	expr := q.Text
	if !q.Regex {
		expr = regexp.QuoteMeta(expr)
	}
	if !q.MatchCase {
		expr = "(?i)" + expr
	}
	pattern, err := regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("正则表达式无效：%v", err)
	}
	return pattern, nil
}

func slashRel(root, path string) string {
	rel, _ := filepath.Rel(root, path)
	return filepath.ToSlash(rel)
}

// searchContentRipgrep runs one ripgrep process for the whole query and
// returns ok=false when rg is unavailable or fails, letting the Go walker
// serve as the portable fallback.
func searchContentRipgrep(ctx context.Context, root string, q ContentQuery) ([]ContentMatch, bool, bool) {
	rg, err := exec.LookPath("rg")
	if err != nil {
		return nil, false, false
	}
	includes, err := rgPatterns(q.Include, true)
	if err != nil {
		return nil, false, false
	}
	excludes, err := rgPatterns(q.Exclude, false)
	if err != nil {
		return nil, false, false
	}
	args := []string{"--json"}
	if !q.WholeWord {
		args = append(args, "--max-count", fmt.Sprint(contentSearchMaxFileMatch+1))
	}
	if !q.MatchCase {
		args = append(args, "-i")
	}
	if !q.Regex {
		args = append(args, "-F")
	}
	if !q.SearchIgnored {
		for dir := range searchSkipDirs {
			args = append(args, "-g", "!"+dir)
		}
	} else {
		args = append(args, "--no-ignore")
	}
	for _, p := range includes {
		args = append(args, "--iglob", p)
	}
	for _, p := range excludes {
		args = append(args, "--iglob", "!"+p)
	}
	args = append(args, "-e", q.Text, ".")
	searchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(searchCtx, rg, args...)
	cmd.Dir = root
	output, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return nil, false, false
	}
	decoder := json.NewDecoder(output)
	var matches []ContentMatch
	truncated := false
	perFile := map[string]int{}
	for {
		var ev rgEvent
		if err := decoder.Decode(&ev); err != nil {
			if err != io.EOF {
				cancel()
				_ = cmd.Wait()
				return nil, false, false
			}
			break
		}
		if ev.Type != "match" {
			continue
		}
		line := strings.TrimSuffix(ev.Data.Lines.Text, "\n")
		var spans [][]int
		for _, sm := range ev.Data.Submatches {
			if sm.End > len(line) || q.WholeWord && (sm.Start == sm.End || !IsWholeWord(line, sm.Start, sm.End)) {
				continue
			}
			spans = append(spans, []int{sm.Start, sm.End})
			if q.WholeWord && len(spans) == contentSearchMaxLineRanges {
				break
			}
		}
		if len(spans) == 0 {
			continue
		}
		if len(matches) >= contentSearchMaxMatches {
			truncated = true
			cancel()
			break
		}
		path := filepath.Join(root, filepath.FromSlash(ev.Data.Path.Text))
		if perFile[path] >= contentSearchMaxFileMatch {
			truncated = true
			continue
		}
		perFile[path]++
		col := utf8.RuneCountInString(line[:spans[0][0]]) + 1
		text, ranges := matchPreview(line, spans)
		matches = append(matches, ContentMatch{Path: path, Line: ev.Data.LineNumber, Col: col, Text: text, Ranges: ranges})
	}
	if err := cmd.Wait(); err != nil && searchCtx.Err() == nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return nil, false, false
		}
	}
	if ctx.Err() != nil {
		return nil, false, false
	}
	sortSearchMatches(matches, root)
	return matches, truncated, true
}

// sortSearchMatches orders matches the same way walkEntries lists the tree:
// at each path level directories sort before files in natural name order.
func sortSearchMatches(matches []ContentMatch, root string) {
	// Precompute dir-ness of every path prefix: a part that has children in
	// the result set is a directory. Root-level names are compared with
	// directories first, matching ReadDir's ordering.
	hasChildren := map[string]bool{}
	for _, m := range matches {
		rel := slashRel(root, m.Path)
		parts := strings.Split(rel, "/")
		for i := 1; i < len(parts); i++ {
			hasChildren[strings.Join(parts[:i], "/")] = true
		}
	}
	less := func(p, q string) bool {
		pp, qp := strings.Split(p, "/"), strings.Split(q, "/")
		for i := 0; i < len(pp) && i < len(qp); i++ {
			if pp[i] != qp[i] {
				pDir := i < len(pp)-1 || hasChildren[strings.Join(pp[:i+1], "/")]
				qDir := i < len(qp)-1 || hasChildren[strings.Join(qp[:i+1], "/")]
				if pDir != qDir {
					return pDir
				}
				return naturalLess(pp[i], qp[i])
			}
		}
		return len(pp) < len(qp)
	}
	sort.SliceStable(matches, func(i, j int) bool {
		a, b := slashRel(root, matches[i].Path), slashRel(root, matches[j].Path)
		if a != b {
			return less(a, b)
		}
		return matches[i].Line < matches[j].Line
	})
}

type rgEvent struct {
	Type string `json:"type"`
	Data struct {
		Path       struct{ Text string } `json:"path"`
		Lines      struct{ Text string } `json:"lines"`
		LineNumber int                   `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
			End   int `json:"end"`
		} `json:"submatches"`
	} `json:"data"`
}

// rgPatterns translates the raw comma-separated pattern text into ripgrep
// --iglob patterns: "./x" anchors at root as "/x", a name without glob
// metacharacters also covers its subtree as "x/**", matching the walker
// semantics where matching a directory includes its contents.
func rgPatterns(text string, include bool) ([]string, error) {
	var out []string
	for _, raw := range splitPatterns(text) {
		p := filepath.ToSlash(strings.TrimSpace(raw))
		if p == "" {
			continue
		}
		anchored := false
		if rest, ok := strings.CutPrefix(p, "./"); ok {
			anchored, p = true, rest
		} else if rest, ok := strings.CutPrefix(p, "/"); ok {
			anchored, p = true, rest
		}
		p = strings.Trim(p, "/")
		if p == "" || p == "." {
			p = "**"
		}
		if anchored {
			p = "/" + p
		}
		out = append(out, p)
		if !strings.ContainsAny(p, "*?[{") && !strings.HasSuffix(p, "/**") {
			out = append(out, p+"/**")
		}
	}
	return out, nil
}

type glob struct {
	re       *regexp.Regexp
	anchored bool
	prefix   string // literal leading folders of an anchored pattern
}

type globs []glob

func parseGlobs(text string) (globs, error) {
	var result globs
	for _, raw := range splitPatterns(text) {
		p := filepath.ToSlash(strings.TrimSpace(raw))
		if p == "" {
			continue
		}
		anchored := false
		if rest, ok := strings.CutPrefix(p, "./"); ok {
			anchored, p = true, rest
		} else if rest, ok := strings.CutPrefix(p, "/"); ok {
			anchored, p = true, rest
		}
		p = strings.Trim(p, "/")
		if p == "" || p == "." {
			p = "**"
		}
		expr := globExpr(p)
		if !anchored {
			expr = "(?:.*/)?" + expr
		}
		re, err := regexp.Compile("^" + expr + "(?:/.*)?$")
		if err != nil {
			return nil, fmt.Errorf("文件模式无效：%s", strings.TrimSpace(raw))
		}
		g := glob{re: re, anchored: anchored}
		if anchored {
			var literal []string
			for _, part := range strings.Split(p, "/") {
				if strings.ContainsAny(part, "*?[{") {
					break
				}
				literal = append(literal, part)
			}
			g.prefix = strings.Join(literal, "/")
		}
		result = append(result, g)
	}
	return result, nil
}

// splitPatterns splits comma separated globs, keeping commas inside {a,b}.
func splitPatterns(text string) []string {
	var parts []string
	depth, start := 0, 0
	for i, ch := range text {
		switch {
		case ch == '{':
			depth++
		case ch == '}' && depth > 0:
			depth--
		case ch == ',' && depth == 0:
			parts = append(parts, text[start:i])
			start = i + 1
		}
	}
	return append(parts, text[start:])
}

// globExpr translates a glob into a regular expression: "*" and "?" stay
// within one path segment, "**" crosses segments, {a,b} picks alternatives
// and [...] is a character class.
func globExpr(p string) string {
	var b strings.Builder
	braces := 0
	for i := 0; i < len(p); i++ {
		switch ch := p[i]; ch {
		case '*':
			if i+1 < len(p) && p[i+1] == '*' {
				i++
				if i+1 < len(p) && p[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '{':
			braces++
			b.WriteString("(?:")
		case '}':
			if braces == 0 {
				b.WriteString(`\}`)
				continue
			}
			braces--
			b.WriteString(")")
		case ',':
			if braces > 0 {
				b.WriteString("|")
			} else {
				b.WriteString(",")
			}
		case '[':
			end := strings.IndexByte(p[i+1:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := p[i+1 : i+1+end]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += end + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}
	return b.String()
}

func (gs globs) match(rel string) bool {
	for _, g := range gs {
		if g.re.MatchString(rel) {
			return true
		}
	}
	return false
}

// roots narrows the walk to the literal folders of anchored include patterns,
// so "./internal" only reads that folder. Any unanchored pattern needs the
// whole workspace.
func (gs globs) roots(root string) []string {
	if len(gs) == 0 {
		return []string{root}
	}
	var dirs []string
	for _, g := range gs {
		if !g.anchored {
			return []string{root}
		}
		dir := filepath.Join(root, filepath.FromSlash(g.prefix))
		info, err := os.Lstat(dir)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			dir = filepath.Dir(dir)
		}
		if Contains(root, dir) {
			dirs = append(dirs, dir)
		}
	}
	sort.Strings(dirs)
	var result []string
	for _, dir := range dirs {
		if len(result) > 0 && Contains(result[len(result)-1], dir) {
			continue
		}
		result = append(result, dir)
	}
	return result
}

func searchFileContent(path string, pattern *regexp.Regexp, wholeWord bool, limit int) ([]ContentMatch, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, err != nil
	}
	if info.Size() > contentSearchMaxFileBytes {
		return nil, true
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, true
	}
	defer f.Close()
	var matches []ContentMatch
	var line strings.Builder
	lineNo := 0
	buf := make([]byte, 128*1024)
	total := 0
	flush := func() bool {
		appendLineMatch(&matches, path, line.String(), lineNo, pattern, wholeWord)
		line.Reset()
		return len(matches) > limit
	}
	for {
		n, readErr := f.Read(buf)
		chunk := buf[:n]
		if total == 0 && bytes.IndexByte(chunk, 0) >= 0 {
			return nil, false
		}
		total += n
		if total > contentSearchMaxFileBytes {
			return matches, true
		}
		for len(chunk) > 0 {
			i := bytes.IndexByte(chunk, '\n')
			if i < 0 {
				line.Write(chunk)
				break
			}
			line.Write(chunk[:i])
			chunk = chunk[i+1:]
			lineNo++
			if flush() {
				return matches[:limit], true
			}
		}
		if readErr != nil {
			break
		}
	}
	if line.Len() > 0 {
		lineNo++
		if flush() {
			return matches[:limit], true
		}
	}
	return matches, false
}

// appendLineMatch decodes one raw line (valid UTF-8 only), applies the pattern
// and whole-word filter, and appends the display match when found.
func appendLineMatch(matches *[]ContentMatch, path, raw string, lineNo int, pattern *regexp.Regexp, wholeWord bool) bool {
	line := strings.TrimSuffix(raw, "\r")
	if !utf8.ValidString(line) {
		return false
	}
	var spans [][]int
	candidates := contentSearchMaxLineRanges
	if wholeWord {
		candidates = -1
	}
	for _, span := range pattern.FindAllStringIndex(line, candidates) {
		if span[1] > span[0] && (!wholeWord || IsWholeWord(line, span[0], span[1])) {
			spans = append(spans, span)
			if len(spans) == contentSearchMaxLineRanges {
				break
			}
		}
	}
	if len(spans) == 0 {
		return false
	}
	col := utf8.RuneCountInString(line[:spans[0][0]]) + 1
	text, ranges := matchPreview(line, spans)
	*matches = append(*matches, ContentMatch{Path: path, Line: lineNo, Col: col, Text: text, Ranges: ranges})
	return true
}

func isWordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// IsWholeWord reports whether the byte range in line sits on whole-word
// boundaries, judged by Unicode letters, digits and underscore.
func IsWholeWord(line string, start, end int) bool {
	if before, _ := utf8.DecodeLastRuneInString(line[:start]); start > 0 && isWordRune(before) {
		return false
	}
	if after, _ := utf8.DecodeRuneInString(line[end:]); end < len(line) && isWordRune(after) {
		return false
	}
	return true
}

// matchPreview keeps the matched line readable: trimmed, capped, and centered
// on the first match when the line is longer than the preview budget. The
// returned ranges locate the byte spans within the preview, in runes.
func matchPreview(line string, spans [][]int) (string, []MatchRange) {
	runes := []rune(line)
	start, end := 0, len(runes)
	for start < end && unicode.IsSpace(runes[start]) {
		start++
	}
	for end > start && unicode.IsSpace(runes[end-1]) {
		end--
	}
	lead, trail := start, end
	runeSpans := make([]MatchRange, 0, len(spans))
	byteAt, runeAt := 0, 0
	toRune := func(b int) int {
		runeAt += utf8.RuneCountInString(line[byteAt:b])
		byteAt = b
		return runeAt
	}
	for _, span := range spans {
		s := toRune(span[0])
		runeSpans = append(runeSpans, MatchRange{s, toRune(span[1])})
	}
	if runeSpans[0].Start-start > contentSearchPreviewLeadIn {
		start = runeSpans[0].Start - contentSearchPreviewLeadIn
	}
	end = min(end, start+contentSearchPreviewRunes)
	text := string(runes[start:end])
	offset := 0
	if start > lead {
		text = "…" + text
		offset = 1
	}
	if end < trail {
		text += "…"
	}
	var ranges []MatchRange
	for _, r := range runeSpans {
		s, e := max(r.Start, start), min(r.End, end)
		if e > s {
			ranges = append(ranges, MatchRange{s - start + offset, e - start + offset})
		}
	}
	return text, ranges
}
