package ui

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	native "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type syntaxToken struct {
	Start, End int
	Type       chroma.TokenType
}

var editorVueLexer = sync.OnceValue(func() chroma.Lexer {
	base := lexers.Get("vue").(*chroma.RegexLexer)
	rules := base.MustRules().Clone()
	for state, entries := range rules {
		for index, entry := range entries {
			entry.Pattern = strings.ReplaceAll(entry.Pattern, `("[\S ]+")`, `("[^"]*")`)
			entry.Pattern = strings.ReplaceAll(entry.Pattern, `(=".+")`, `(="[^"]*")`)
			rules[state][index] = entry
		}
	}
	var embedded []chroma.Rule
	for _, section := range []struct{ tag, language, lexer string }{
		{"script", "tsx", "tsx"},
		{"script", "(?:ts|typescript)", "typescript"},
		{"script", "jsx", "jsx"},
		{"style", "scss", "scss"},
		{"style", "sass", "sass"},
		{"style", "less", "less"},
	} {
		pattern := `(?i)(<` + section.tag + `\b(?=[^>]*\blang\s*=\s*["']` + section.language + `["'])[^>]*>)(.*?)(</` + section.tag + `\s*>)`
		embedded = append(embedded, chroma.Rule{Pattern: pattern, Type: chroma.ByGroups(chroma.UsingSelf("root"), chroma.Using(section.lexer), chroma.UsingSelf("root"))})
	}
	for _, section := range []struct{ tag, lexer string }{{"script", "javascript"}, {"style", "css"}} {
		pattern := `(?i)(<` + section.tag + `\b[^>]*>)(.*?)(</` + section.tag + `\s*>)`
		embedded = append(embedded, chroma.Rule{Pattern: pattern, Type: chroma.ByGroups(chroma.UsingSelf("root"), chroma.Using(section.lexer), chroma.UsingSelf("root"))})
	}
	rules["root"] = append(embedded, rules["root"]...)
	config := *base.Config()
	return chroma.Coalesce(chroma.MustNewLexer(&config, func() chroma.Rules { return rules }).SetRegistry(lexers.GlobalLexerRegistry))
})

func editorLexer(path string) chroma.Lexer {
	name := strings.ToLower(filepath.Base(path))
	var lexer chroma.Lexer
	switch name {
	case "go.mod", "go.work", "go.sum":
		return nil
	case "brewfile":
		lexer = lexers.Get("ruby")
	case ".npmrc", ".yarnrc":
		lexer = lexers.Get("ini")
	case "makefile", "gnumakefile":
		lexer = lexers.Get("make")
	case "dockerfile":
		lexer = lexers.Get("docker")
	default:
		switch filepath.Ext(name) {
		case ".vue":
			return editorVueLexer()
		case ".sql":
			lexer = lexers.Get("sql")
		default:
			lexer = lexers.Match(filepath.Base(path))
		}
	}
	if lexer == nil {
		return nil
	}
	return chroma.Coalesce(lexer)
}

type editorPalette struct {
	background, text, muted, selection, border         native.Color
	added, deleted, addedBackground, deletedBackground native.Color
	accent                                             native.Color
	style                                              *chroma.Style
	codeColors                                         *editor.Theme
}

func (a *App) activeEditorPalette(theme *native.Theme) editorPalette {
	return a.activePalette(theme).editorFor(theme.Dark)
}

func (p palette) editorFor(dark bool) editorPalette {
	if p.editor.style == nil {
		return githubEditorPalette(dark)
	}
	return p.editor
}

func claudeEditorPalette(dark bool) editorPalette {
	styleName := "claude-light"
	background, text, gutter := "#fdfcfa", "#1a1918", "#a9a7a1"
	border, accent, added, deleted := "#dcdad4", "#c4613f", "#2c7a39", "#c8374f"
	selection := native.RGBA(87, 105, 247, 0.16)
	keyword, typeName, function := "#4c5ee0", "#0f6e6e", "#b0532f"
	str, number, comment := "#2c7a39", "#966c1e", "#74726d"
	operator, punctuation, builtin, tag := "#66645f", "#83817b", "#7a4fe0", "#c8207c"
	if dark {
		styleName = "claude-dark"
		background, text, gutter = "#1a1917", "#ecebe6", "#5c5a55"
		border, accent, added, deleted = "#34322f", "#d77757", "#4eba65", "#ff6b80"
		selection = native.RGBA(177, 185, 249, 0.24)
		keyword, typeName, function = "#b1b9f9", "#5fb3a6", "#f0a585"
		str, number, comment = "#8fcf8f", "#f2c55c", "#7a7873"
		operator, punctuation, builtin, tag = "#9a9893", "#7f7d78", "#af87ff", "#fd5db1"
	}
	palette := editorPalette{
		background: native.Hex(background), text: native.Hex(text), muted: native.Hex(gutter),
		border: native.Hex(border), accent: native.Hex(accent),
		added: native.Hex(added), deleted: native.Hex(deleted),
		style: chroma.MustNewStyle(styleName, chroma.StyleEntries{
			chroma.Text: text, chroma.Keyword: keyword, chroma.KeywordType: typeName,
			chroma.NameClass: typeName, chroma.NameFunction: function, chroma.NameBuiltin: builtin,
			chroma.NameConstant: number, chroma.NameTag: tag, chroma.NameAttribute: number,
			chroma.LiteralString: str, chroma.LiteralNumber: number, chroma.Comment: comment,
			chroma.Operator: operator, chroma.Punctuation: punctuation,
			chroma.GenericHeading: accent, chroma.GenericSubheading: accent,
			chroma.GenericDeleted: deleted, chroma.GenericInserted: added,
			chroma.GenericStrong: keyword, chroma.GenericEmph: keyword, chroma.Error: deleted,
		}),
	}
	palette.selection = selection.Over(palette.background)
	palette.addedBackground = palette.added.Alpha(0.12).Over(palette.background)
	palette.deletedBackground = palette.deleted.Alpha(0.12).Over(palette.background)
	if !dark {
		palette.codeColors = palette.markdownCodeColors()
	}
	return palette
}

func (palette editorPalette) markdownCodeColors() *editor.Theme {
	colors := &editor.Theme{Text: palette.text}
	for kind, token := range map[editor.TokenKind]chroma.TokenType{
		editor.TokenKeyword: chroma.Keyword, editor.TokenType: chroma.NameClass,
		editor.TokenFunction: chroma.NameFunction, editor.TokenVariable: chroma.NameVariable,
		editor.TokenProperty: chroma.NameProperty, editor.TokenConstant: chroma.NameConstant,
		editor.TokenNumber: chroma.LiteralNumber, editor.TokenString: chroma.LiteralString,
		editor.TokenEscape: chroma.LiteralStringEscape, editor.TokenRegexp: chroma.LiteralStringRegex,
		editor.TokenComment: chroma.Comment, editor.TokenOperator: chroma.Operator,
		editor.TokenPunctuation: chroma.Punctuation, editor.TokenTag: chroma.NameTag,
		editor.TokenAttribute: chroma.NameAttribute, editor.TokenMeta: chroma.CommentPreproc,
		editor.TokenHeading: chroma.GenericHeading, editor.TokenEmphasis: chroma.GenericEmph,
		editor.TokenStrong: chroma.GenericStrong, editor.TokenLink: chroma.NameTag,
		editor.TokenCode: chroma.LiteralString, editor.TokenInserted: chroma.GenericInserted,
		editor.TokenDeleted: chroma.GenericDeleted, editor.TokenInvalid: chroma.Error,
	} {
		colors.Tokens[kind].Color = palette.syntaxColor(token)
	}
	return colors
}

func githubEditorPalette(dark bool) editorPalette {
	if dark {
		return editorPalette{
			background: native.Hex("#0d1117"), text: native.Hex("#e6edf3"), muted: native.Hex("#8b949e"),
			selection: native.Hex("#264f78"), border: native.Hex("#30363d"), accent: native.Hex("#58a6ff"),
			added: native.Hex("#3fb950"), deleted: native.Hex("#f85149"),
			addedBackground: native.Hex("#12261e"), deletedBackground: native.Hex("#2c171c"),
			style: styles.Get("github-dark"),
		}
	}
	return editorPalette{
		background: native.Hex("#ffffff"), text: native.Hex("#1f2328"), muted: native.Hex("#57606a"),
		selection: native.Hex("#b6d7fb"), border: native.Hex("#d0d7de"), accent: native.Hex("#0969da"),
		added: native.Hex("#1a7f37"), deleted: native.Hex("#cf222e"),
		addedBackground: native.Hex("#dafbe1"), deletedBackground: native.Hex("#ffebe9"),
		style: styles.Get("github"),
	}
}

func (palette editorPalette) area(area native.Element) native.Element {
	return area.Background(palette.background).TextColor(palette.text).SelectionColor(palette.selection)
}

func (palette editorPalette) lineTheme() *native.Theme {
	return &native.Theme{TextMuted: palette.muted, Success: palette.added, Danger: palette.deleted}
}

func (palette editorPalette) syntaxColor(token chroma.TokenType) native.Color {
	color := palette.style.Get(token).Colour
	if color.IsSet() {
		return native.RGB(color.Red(), color.Green(), color.Blue())
	}
	return palette.text
}

func (palette editorPalette) ranges(tokens []syntaxToken, start, end int) []native.TextRange {
	var ranges []native.TextRange
	for _, token := range tokens {
		if token.End <= start || token.Start >= end {
			continue
		}
		ranges = append(ranges, native.TextRange{Start: token.Start, End: token.End, Color: palette.syntaxColor(token.Type)})
	}
	return ranges
}
