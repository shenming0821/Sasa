package ui

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	native "github.com/egoist/mygo/ui"
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

func (palette editorPalette) ranges(tokens []syntaxToken, start, end int) []native.TextRange {
	var ranges []native.TextRange
	for _, token := range tokens {
		if token.End <= start || token.Start >= end {
			continue
		}
		entry := palette.style.Get(token.Type)
		color := palette.text
		if entry.Colour.IsSet() {
			color = native.RGB(entry.Colour.Red(), entry.Colour.Green(), entry.Colour.Blue())
		}
		ranges = append(ranges, native.TextRange{Start: token.Start, End: token.End, Color: color})
	}
	return ranges
}
