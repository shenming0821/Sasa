package ui

import (
	"path/filepath"
	"strings"

	native "github.com/egoist/mygo/ui"
)

var (
	filePlainGlyph      = fileGlyph(`<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/>`)
	fileCodeGlyph       = fileGlyph(`<path d="M10 12.5 8 15l2 2.5"/><path d="m14 12.5 2 2.5-2 2.5"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/>`)
	fileTextGlyph       = fileGlyph(`<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M10 9H8"/><path d="M16 13H8"/><path d="M16 17H8"/>`)
	fileJSONGlyph       = fileGlyph(`<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M10 12a1 1 0 0 0-1 1v1a1 1 0 0 1-1 1 1 1 0 0 1 1 1v1a1 1 0 0 0 1 1"/><path d="M14 18a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1 1 1 0 0 1-1-1v-1a1 1 0 0 0-1-1"/>`)
	fileImageGlyph      = fileGlyph(`<rect width="18" height="18" x="3" y="3" rx="2" ry="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21"/>`)
	fileSettingsGlyph   = fileGlyph(`<path d="M20 7h-9"/><path d="M14 17H5"/><circle cx="17" cy="17" r="3"/><circle cx="7" cy="7" r="3"/>`)
	fileLockGlyph       = fileGlyph(`<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>`)
	fileTerminalGlyph   = fileGlyph(`<path d="m4 17 6-6-6-6"/><path d="M12 19h8"/>`)
	fileFolderGlyph     = fileGlyph(`<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>`)
	fileFolderOpenGlyph = fileGlyph(`<path d="m6 14 1.5-2.9A2 2 0 0 1 9.24 10H20a2 2 0 0 1 1.94 2.5l-1.54 6a2 2 0 0 1-1.95 1.5H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h3.9a2 2 0 0 1 1.69.9l.81 1.2a2 2 0 0 0 1.67.9H18a2 2 0 0 1 2 2v2"/>`)
)

func fileGlyph(paths string) *native.SVG {
	return native.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

func pathFileIcon(theme *native.Theme, path string) (*native.SVG, native.Color) {
	lower := strings.ToLower(filepath.Base(path))
	ext := strings.TrimPrefix(filepath.Ext(lower), ".")
	color := func(dark, light string) native.Color {
		if theme.Dark {
			return native.Hex(dark)
		}
		return native.Hex(light)
	}
	switch lower {
	case "go.mod", "go.sum", "go.work":
		return fileCodeGlyph, color("#00add8", "#00809f")
	case "package.json", "tsconfig.json", "bun.lock", "package-lock.json", "yarn.lock", "pnpm-lock.yaml":
		return fileJSONGlyph, color("#cbcb41", "#9a8a00")
	case "makefile", "dockerfile", "justfile":
		return fileSettingsGlyph, color("#e37933", "#b5571a")
	case "license", "license.md", "license.txt":
		return fileTextGlyph, color("#d4af37", "#a07d10")
	}
	if strings.HasPrefix(lower, ".env") {
		return fileLockGlyph, color("#e8c482", "#a86a0c")
	}
	if strings.HasPrefix(lower, ".") && ext == strings.TrimPrefix(lower, ".") {
		return fileSettingsGlyph, theme.TextMuted
	}
	switch ext {
	case "go":
		return fileCodeGlyph, color("#00add8", "#00809f")
	case "ts", "tsx", "mts", "cts":
		return fileCodeGlyph, color("#4d8fdf", "#2f6fc0")
	case "js", "jsx", "mjs", "cjs":
		return fileCodeGlyph, color("#e8d44d", "#a68f00")
	case "rs":
		return fileCodeGlyph, color("#dea584", "#b0643a")
	case "py":
		return fileCodeGlyph, color("#5a9fd4", "#3572a5")
	case "rb":
		return fileCodeGlyph, color("#e0525a", "#c0303a")
	case "swift":
		return fileCodeGlyph, color("#f0814b", "#d0602a")
	case "c", "h", "cpp", "cc", "hpp", "m", "mm":
		return fileCodeGlyph, color("#7aa6da", "#4a76b0")
	case "java", "kt", "kts", "scala":
		return fileCodeGlyph, color("#e76f51", "#c04a2b")
	case "html", "htm", "vue", "svelte", "astro":
		return fileCodeGlyph, color("#e8714a", "#c4492a")
	case "css", "scss", "sass", "less":
		return fileCodeGlyph, color("#a074c4", "#7a4fa8")
	case "sh", "bash", "zsh", "fish":
		return fileTerminalGlyph, color("#89e051", "#4f9a1f")
	case "json", "jsonc", "json5":
		return fileJSONGlyph, color("#cbcb41", "#9a8a00")
	case "yaml", "yml", "toml", "ini", "conf", "cfg", "xml", "plist":
		return fileSettingsGlyph, color("#c9a0dc", "#8a5aa8")
	case "md", "mdx", "markdown", "txt", "rst":
		return fileTextGlyph, color("#6ba4d8", "#3d78ad")
	case "png", "jpg", "jpeg", "gif", "webp", "svg", "ico", "icns", "bmp":
		return fileImageGlyph, color("#a6c56a", "#5f8a26")
	case "lock", "sum":
		return fileLockGlyph, theme.TextMuted.Alpha(.7)
	}
	return filePlainGlyph, theme.TextMuted
}

func drawFileIcon(c *native.Context, path string, size float32, ignored bool) {
	glyph, tint := pathFileIcon(c.Theme(), path)
	if ignored {
		tint = tint.Alpha(.5)
	}
	native.Icon(c, glyph).Size(size, size).Shrink(0).TextColor(tint)
}

func drawFileTreeFolderIcon(c *native.Context, size float32, expanded, ignored bool) {
	glyph, tint := fileFolderGlyph, c.Theme().Accent
	if expanded {
		glyph = fileFolderOpenGlyph
	}
	if ignored {
		tint = tint.Alpha(.5)
	}
	native.Icon(c, glyph).Size(size, size).Shrink(0).TextColor(tint)
}
