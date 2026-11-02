package ui

import (
	"context"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Fuyu/internal/git"
)

func TestEditorLanguageTokens(t *testing.T) {
	for _, sample := range []struct {
		path, source, word string
		kind               chroma.TokenType
	}{
		{"source.go", "package main\nfunc hello() string { return \"你好\" }", "hello", chroma.NameFunction},
		{"source.js", "const value = \"你好\";", "const", chroma.Keyword},
		{"source.ts", "interface Person { name: string }", "interface", chroma.Keyword},
		{"source.jsx", "const View = () => <div title=\"你好\" />;", "div", chroma.NameTag},
		{"source.tsx", "const View = () => <div title=\"你好\" />;", "div", chroma.NameTag},
		{"source.vue", "<template><div class=\"hello\">你好</div></template>", "class", chroma.NameAttribute},
		{"source.html", "<div title=\"你好\">text</div>", "div", chroma.NameTag},
		{"source.css", ".hello { color: red; }", "color", chroma.Keyword},
		{"source.json", "{\"name\": \"你好\", \"value\": 42}", "\"name\"", chroma.NameTag},
		{"source.yaml", "name: 你好\nvalue: 42\n", "name", chroma.NameTag},
		{"source.py", "def hello():\n    return \"你好\"\n", "hello", chroma.NameFunction},
		{"source.sh", "#!/bin/sh\necho \"你好\"\n", "echo", chroma.NameBuiltin},
		{"source.sql", "SELECT name FROM people WHERE id = 42;", "SELECT", chroma.Keyword},
		{"source.md", "# Heading\n\n**hello**\n", "# Heading", chroma.GenericHeading},
		{"Dockerfile", "FROM alpine\nRUN echo hello\n", "FROM", chroma.Keyword},
		{"Makefile", "all:\n\techo hello\n", "all", chroma.NameFunction},
		{"Brewfile", "brew \"git\"\n", "\"git\"", chroma.LiteralString},
		{".npmrc", "registry=https://example.com\n", "registry", chroma.NameAttribute},
	} {
		t.Run(sample.path, func(t *testing.T) {
			lexer := editorLexer(sample.path)
			if lexer == nil {
				t.Fatal("language has no lexer")
			}
			tokens := highlightText(context.Background(), lexer, sample.source, 0)
			start := utf8.RuneCountInString(sample.source[:strings.Index(sample.source, sample.word)])
			found := false
			for _, token := range tokens {
				if token.Start <= start && token.End > start && (token.Type == sample.kind || token.Type.InSubCategory(sample.kind)) {
					found = true
				}
			}
			if !found {
				t.Fatalf("%q did not receive %s: %#v", sample.word, sample.kind, tokens)
			}
			for _, dark := range []bool{false, true} {
				ranges := githubEditorPalette(dark).ranges(tokens, 0, utf8.RuneCountInString(sample.source))
				if len(ranges) != len(tokens) {
					t.Fatal("token lost its theme color")
				}
			}
		})
	}
	for _, path := range []string{"source.unknown-fuyu", "go.mod", "go.work", "go.sum"} {
		if editorLexer(path) != nil {
			t.Fatalf("%s received an unrelated lexer", path)
		}
	}
}

func TestEditorVueDirectiveStopsAtClosingQuote(t *testing.T) {
	source := `<template>
  <div v-if="
      ready &&
      enabled
    "
    v-show="visible"
    class="hello">你好</div>
</template>
<script>
const greeting = "你好";
</script>
<style>
.hello { color: red; }
</style>`
	tokenAt := func(lexer chroma.Lexer, word string) chroma.TokenType {
		start := utf8.RuneCountInString(source[:strings.Index(source, word)])
		for _, token := range highlightText(context.Background(), lexer, source, 0) {
			if token.Start <= start && token.End > start {
				return token.Type
			}
		}
		return chroma.Text
	}
	if tokenAt(lexers.Match("source.vue"), "v-show") != chroma.LiteralString {
		t.Fatal("fixture no longer reproduces the upstream directive overrun")
	}
	for word, kind := range map[string]chroma.TokenType{"v-show": chroma.NameTag, "class": chroma.NameAttribute, "const": chroma.KeywordReserved, "color": chroma.Keyword} {
		if got := tokenAt(editorLexer("source.vue"), word); got != kind {
			t.Errorf("%q: got %s, want %s", word, got, kind)
		}
	}
}

func TestEditorVueEmbeddedLanguages(t *testing.T) {
	for _, sample := range []struct {
		lang, body, word string
		kind             chroma.TokenType
	}{
		{"ts", "interface Person { name: string }", "interface", chroma.KeywordReserved},
		{"tsx", "const View = () => <div />;", "div", chroma.NameTag},
		{"jsx", "const View = () => <div />;", "div", chroma.NameTag},
	} {
		source := "<script setup lang=\"" + sample.lang + "\">\n" + sample.body + "\n</script>"
		tokens := highlightText(context.Background(), editorLexer("source.vue"), source, 0)
		start := strings.Index(source, sample.word)
		found := false
		for _, token := range tokens {
			if token.Start <= start && token.End > start && token.Type == sample.kind {
				found = true
			}
		}
		if !found {
			t.Errorf("Vue %s did not delegate %q: %#v", sample.lang, sample.word, tokens)
		}
	}
}

func TestEditorTokenOffsetsAndThemeInheritance(t *testing.T) {
	source := "/* 中文\n注释 */\nconst value = `你好\n世界`;\n"
	tokens := highlightText(context.Background(), editorLexer("source.js"), source, 17)
	for _, token := range tokens {
		if token.Start < 17 || token.End > 17+utf8.RuneCountInString(source) || token.End <= token.Start {
			t.Fatal("invalid rune range", token)
		}
	}
	for _, dark := range []bool{false, true} {
		palette := githubEditorPalette(dark)
		types := []chroma.TokenType{chroma.Keyword, chroma.KeywordType, chroma.NameFunction, chroma.NameClass, chroma.NameConstant, chroma.NameAttribute, chroma.NameTag, chroma.NameVariableInstance, chroma.Operator, chroma.CommentMultiline, chroma.LiteralStringDouble, chroma.LiteralNumberInteger, chroma.NameProperty, chroma.Punctuation, chroma.GenericHeading}
		for _, kind := range types {
			ranges := palette.ranges([]syntaxToken{{Start: 2, End: 5, Type: kind}}, 0, 6)
			entry := palette.style.Get(kind)
			expected := palette.text
			if entry.Colour.IsSet() {
				expected = native.RGB(entry.Colour.Red(), entry.Colour.Green(), entry.Colour.Blue())
			}
			if len(ranges) != 1 || ranges[0].Color != expected {
				t.Fatal("lost inherited style", kind, ranges)
			}
		}
	}
	if !reflect.DeepEqual(tokens, highlightText(context.Background(), editorLexer("source.js"), source, 17)) {
		t.Fatal("tokenization is unstable")
	}
}

func TestEditorGitHubThemesAcrossViews(t *testing.T) {
	for _, mode := range []string{"file", "inline", "split"} {
		t.Run(mode, func(t *testing.T) {
			a, queue, root := p4App(t)
			d := &filePage{id: "theme", root: root, path: filepath.Join(root, "source.go"), kind: "file", loaded: true, buffer: native.NewTextBuffer("package main\n\nfunc greeting() string {\n\treturn \"你好，Fuyu\"\n}\n")}
			if mode != "file" {
				d.kind, d.gitSplit = "diff", mode == "split"
				patch := "@@ -1,5 +1,5 @@\n package main\n \n func greeting() string {\n-\treturn \"old\"\n+\treturn \"你好，Fuyu\"\n }\n"
				review := gitview.ParsePatch(patch)
				a.gitSetDiffText(d, patch, &review)
			}
			d.observed, d.savedText = d.buffer.Snapshot(), d.buffer.String()
			a.files = map[string]*filePage{d.id: d}
			tester := native.NewTester(func(c *native.Context) {
				native.Row(c).Fill().Children(func() { a.fileView(c, d, false, false) })
			}, 1000, 600)
			p4Wait(t, tester, queue, func() bool { return !d.highlighting && d.contentWidth > 0 })
			original := append([]syntaxToken(nil), d.highlights...)
			version := d.buffer.Version()
			for _, dark := range []bool{false, true, false, true} {
				tester.SetDark(dark)
				palette := githubEditorPalette(dark)
				label := "文件内容 " + d.path
				if mode == "split" {
					label = "差异 原版本"
				}
				area, ok := tester.Find(label)
				if !ok {
					t.Fatal("missing code area")
				}
				image := tester.Image()
				pixel := image.RGBAAt(int(area.X+area.W-30), int(area.Y+area.H-30))
				if pixel != (color.RGBA{R: palette.background.R, G: palette.background.G, B: palette.background.B, A: 255}) {
					t.Fatal("code background did not follow theme", dark, pixel)
				}
				keyword := palette.ranges([]syntaxToken{{Start: 0, End: 7, Type: chroma.KeywordNamespace}}, 0, 10)[0].Color
				colors := map[native.Color]bool{keyword: false, palette.muted: false}
				if mode != "file" {
					colors[palette.deletedBackground] = false
				}
				for y := int(area.Y); y < int(area.Y+area.H) && y < image.Bounds().Max.Y; y++ {
					for x := int(area.X); x < int(area.X+area.W) && x < image.Bounds().Max.X; x++ {
						pixel := image.RGBAAt(x, y)
						candidate := native.RGB(pixel.R, pixel.G, pixel.B)
						if _, tracked := colors[candidate]; tracked {
							colors[candidate] = true
						}
					}
				}
				for tracked, found := range colors {
					if !found {
						t.Errorf("theme=%v missing rendered color %#v", dark, tracked)
					}
				}
				if d.highlighting || d.buffer.Version() != version || !reflect.DeepEqual(original, d.highlights) || d.dirty() {
					t.Fatal("appearance change altered the document or restarted lexing")
				}
				if directory := os.Getenv("FUYU_EDITOR_THEME_PREVIEW"); directory != "" {
					if err := os.MkdirAll(directory, 0700); err != nil {
						t.Fatal(err)
					}
					file, err := os.Create(filepath.Join(directory, fmt.Sprintf("%s-dark-%v.png", mode, dark)))
					if err != nil {
						t.Fatal(err)
					}
					encodeErr, closeErr := png.Encode(file, image), file.Close()
					if encodeErr != nil || closeErr != nil {
						t.Fatal(encodeErr, closeErr)
					}
				}
			}
		})
	}
}

func TestEditorThemeKeepsDirtySelectionAndScroll(t *testing.T) {
	a, queue, root := p4App(t)
	a.files = map[string]*filePage{}
	documents := []*filePage{}
	for _, name := range []string{"first.go", "second.js"} {
		text := strings.Repeat("var value = \"中文\";\n", 80)
		document := &filePage{id: name, root: root, path: filepath.Join(root, name), kind: "file", loaded: true, buffer: native.NewTextBuffer(text), savedText: "baseline\n"}
		document.observed = document.buffer.Snapshot()
		a.files[document.id] = document
		documents = append(documents, document)
	}
	active := documents[0]
	tester := native.NewTester(func(c *native.Context) {
		native.Row(c).Fill().Children(func() { a.fileView(c.Key(active.id), active, false, false) })
	}, 1000, 600)
	for _, document := range documents {
		active = document
		tester.Frame()
		p4Wait(t, tester, queue, func() bool { return !active.highlighting && active.contentWidth > 0 })
		start, _ := active.buffer.Snapshot().LineRange(35)
		active.pendingSelection = &[2]int{start, start + 5}
		tester.Frame()
		tester.Frame()
		anchor, caret, scroll := active.anchor, active.caret, active.scroll.Y
		if anchor != start || caret != start+5 || scroll == 0 || !active.dirty() {
			t.Fatal("fixture did not establish a dirty scrolled selection", anchor, caret, scroll)
		}
		for _, dark := range []bool{true, false, true, false} {
			tester.SetDark(dark)
			if active.anchor != anchor || active.caret != caret || active.scroll.Y != scroll || !active.dirty() || active.highlighting {
				t.Fatal("theme changed editing state")
			}
		}
	}
}

func TestEditorThemeSelectionAndCaretPixels(t *testing.T) {
	for _, dark := range []bool{false, true} {
		palette := githubEditorPalette(dark)
		buffer := native.NewTextBuffer("hello world\n")
		tester := native.NewTester(func(c *native.Context) {
			palette.area(native.TextAreaBufferBase(c, buffer)).Fill().Padding(10).FixedLineHeight(20).AutoFocus()
		}, 300, 100)
		tester.ClickAt(11, 20)
		caret, ok := tester.TextCaret()
		if !ok {
			t.Fatal("missing caret")
		}
		pixel := tester.Image().RGBAAt(int(caret.X), int(caret.Y+caret.H/2))
		if pixel.R != palette.text.R || pixel.G != palette.text.G || pixel.B != palette.text.B {
			t.Fatal("caret did not use theme text color", pixel)
		}
		tester.Key(native.Cmd, native.KeyA)
		image := tester.Image()
		found := false
		for y := 10; y < 30; y++ {
			for x := 10; x < 70; x++ {
				pixel := image.RGBAAt(x, y)
				if pixel.R == palette.selection.R && pixel.G == palette.selection.G && pixel.B == palette.selection.B {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("selection did not use editor palette")
		}
	}
}
