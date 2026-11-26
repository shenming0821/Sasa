package ui

import (
	"fmt"

	"github.com/alecthomas/chroma/v2"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/terminal"
)

func qoderLightPalette() palette {
	colors := palette{
		background: native.Hex("#ffffff"), surface: native.Hex("#f2f4f2"),
		surfaceHover: native.Hex("#eaeeea"), surfacePressed: native.Hex("#dee5df"),
		border: native.Hex("#e3e6e3"), text: native.Hex("#141414"), textMuted: native.Hex("#636261"),
		accent: native.Hex("#4b6f5a"), accentText: native.Hex("#fdfdfd"),
		accentHover: native.Hex("#436651"), accentPressed: native.Hex("#3a5747"),
		selection: native.RGBA(75, 111, 90, .22), focus: native.RGBA(75, 111, 90, .50),
		search:         searchPalette{native.RGBA(239, 176, 65, .24), native.RGBA(239, 176, 65, .44), native.RGBA(183, 122, 10, .90)},
		pickerSelected: native.Hex("#dbe6de"),
		status:         statusPalette{accent: native.Hex("#4b6f5a"), modified: native.Hex("#b77a0a"), danger: native.Hex("#c9363e"), added: native.Hex("#2e7d4f")},
		chat:           paletteChat{userBubble: native.Hex("#eaf0eb"), composerCard: native.Hex("#f8faf8")},
	}
	return colors.withCodeAndTerminal("qoder-light", false, "#141414", colors.background, chroma.StyleEntries{
		chroma.Keyword: "#477a61", chroma.NameFunction: "#427db5", chroma.NameClass: "#9870b4",
		chroma.LiteralString: "#ba7b37", chroma.LiteralNumber: "#b7647b", chroma.Comment: "#848c86",
		chroma.Operator: "#555b57", chroma.Punctuation: "#6c726d",
		chroma.NameConstant: "#b7647b", chroma.NameBuiltin: "#9870b4", chroma.NameTag: "#b7647b",
		chroma.NameAttribute: "#9870b4", chroma.GenericHeading: "#477a61",
	}, [16]uint32{
		0x24262b, 0xc9363e, 0x2e7d4f, 0x8a5a00, 0x0969a8, 0x8e4e8b, 0x0b7f78, 0xa7ada8,
		0x727a74, 0xe04b53, 0x3f9a64, 0xb77a0a, 0x2b83c1, 0xaa69a7, 0x249d94, 0xd5dbd8,
	})
}

func qoderDarkPalette() palette {
	colors := palette{
		background: native.Hex("#191b1a"), surface: native.Hex("#222523"),
		surfaceHover: native.Hex("#2d332e"), surfacePressed: native.Hex("#363d37"),
		border: native.Hex("#363c37"), text: native.Hex("#d9dcd9"), textMuted: native.Hex("#949d96"),
		accent: native.Hex("#8ee5a1"), accentText: native.Hex("#080807"),
		accentHover: native.Hex("#a0edb1"), accentPressed: native.Hex("#73cd94"),
		selection: native.RGBA(77, 156, 97, .32), focus: native.RGBA(142, 229, 161, .50),
		search:         searchPalette{native.RGBA(250, 173, 20, .20), native.RGBA(250, 173, 20, .38), native.RGBA(250, 173, 20, .85)},
		pickerSelected: native.Hex("#304438"),
		status:         statusPalette{accent: native.Hex("#8ee5a1"), modified: native.Hex("#faad14"), danger: native.Hex("#ff6b70"), added: native.Hex("#73cd94")},
		chat:           paletteChat{userBubble: native.Hex("#272f29"), composerCard: native.Hex("#202421")},
	}
	return colors.withCodeAndTerminal("qoder-dark", true, "#d9dcd9", colors.background, chroma.StyleEntries{
		chroma.Keyword: "#7cb196", chroma.NameFunction: "#7fb0e0", chroma.NameClass: "#c4a3dc",
		chroma.LiteralString: "#dcaa72", chroma.LiteralNumber: "#e095aa", chroma.Comment: "#758077",
		chroma.Operator: "#a3aba5", chroma.Punctuation: "#8a948c",
		chroma.NameConstant: "#e095aa", chroma.NameBuiltin: "#c4a3dc", chroma.NameTag: "#e095aa",
		chroma.NameAttribute: "#c4a3dc", chroma.GenericHeading: "#7cb196",
	}, [16]uint32{
		0x2b302c, 0xff6b70, 0x73cd94, 0xffd666, 0x75beff, 0xd59bd1, 0x79e0d4, 0xf4f5f3,
		0x8b918d, 0xff8a8e, 0x9be6b3, 0xffe58f, 0xa3d3ff, 0xe6b9e2, 0xa2ede5, 0xffffff,
	})
}

func cursorLightPalette() palette {
	colors := palette{
		background: native.Hex("#fcfcfc"), surface: native.Hex("#f3f3f3"),
		surfaceHover: native.Hex("#ededed"), surfacePressed: native.Hex("#e3e3e3"),
		border: native.Hex("#e2e2e2"), text: native.Hex("#141414"), textMuted: native.Hex("#6d6d6d"),
		accent: native.Hex("#2778c1"), accentText: native.Hex("#fcfcfc"),
		accentHover: native.Hex("#246aab"), accentPressed: native.Hex("#1e5e96"),
		selection: native.RGBA(20, 20, 20, .12), focus: native.RGBA(39, 120, 193, .50),
		search:         searchPalette{native.RGBA(59, 126, 132, .141), native.RGBA(59, 126, 132, .239), native.RGBA(59, 126, 132, .90)},
		pickerSelected: native.Hex("#dedede"),
		status:         statusPalette{accent: native.Hex("#2778c1"), modified: native.Hex("#a46700"), danger: native.Hex("#be1744"), added: native.Hex("#007041")},
		chat:           paletteChat{userBubble: native.Hex("#ededed"), composerCard: native.Hex("#ffffff")},
	}
	return colors.withCodeAndTerminal("cursor-light", false, "#141414", colors.surface, chroma.StyleEntries{
		chroma.Keyword: "#a30034", chroma.NameFunction: "#cd4500", chroma.NameClass: "#005293",
		chroma.LiteralString: "#7565cc", chroma.LiteralNumber: "#92156a", chroma.Comment: "#717171",
		chroma.Operator: "#141414", chroma.Punctuation: "#505050",
		chroma.NameConstant: "#005293", chroma.NameBuiltin: "#3b7e84", chroma.NameTag: "#007041",
		chroma.NameAttribute: "#481b9c", chroma.GenericHeading: "#005293",
	}, [16]uint32{
		0x141414, 0xbe1744, 0x007041, 0x8b5700, 0x0064b0, 0x92156a, 0x176c74, 0xa8a8a8,
		0x505050, 0xce405b, 0x00854c, 0xa46700, 0x2778c1, 0xb54e90, 0x3b7e84, 0xd4d4d4,
	})
}

func cursorDarkPalette() palette {
	colors := palette{
		background: native.Hex("#181818"), surface: native.Hex("#141414"),
		surfaceHover: native.Hex("#242424"), surfacePressed: native.Hex("#2c2c2c"),
		border: native.Hex("#242424"), text: native.Hex("#f0f0f0"), textMuted: native.Hex("#989898"),
		accent: native.Hex("#81a1c1"), accentText: native.Hex("#191c22"),
		accentHover: native.Hex("#87a6c4"), accentPressed: native.Hex("#6e89a4"),
		selection: native.RGBA(64, 64, 64, .60), focus: native.RGBA(129, 161, 193, .55),
		search:         searchPalette{native.RGBA(136, 192, 208, .27), native.RGBA(136, 192, 208, .40), native.RGBA(136, 192, 208, .85)},
		pickerSelected: native.Hex("#333333"),
		status:         statusPalette{accent: native.Hex("#81a1c1"), modified: native.Hex("#f1b467"), danger: native.Hex("#fc6b83"), added: native.Hex("#70b489")},
		chat:           paletteChat{userBubble: native.Hex("#262626"), composerCard: native.Hex("#1d1d1d")},
	}
	return colors.withCodeAndTerminal("cursor-dark", true, "#d6d6dd", colors.surface, chroma.StyleEntries{
		chroma.Keyword: "#82d2ce", chroma.NameFunction: "#efb080", chroma.NameClass: "#87c3ff",
		chroma.LiteralString: "#e394dc", chroma.LiteralNumber: "#ebc88d", chroma.Comment: "#9a9a9a",
		chroma.Operator: "#d6d6dd", chroma.Punctuation: "#a4a4a4",
		chroma.NameConstant: "#82d2ce", chroma.NameBuiltin: "#82d2ce", chroma.NameTag: "#87c3ff",
		chroma.NameAttribute: "#aaa0fa", chroma.GenericHeading: "#aaa0fa",
	}, [16]uint32{
		0x242424, 0xfc6b83, 0x3fa266, 0xd2943e, 0x81a1c1, 0xb48ead, 0x88c0d0, 0xf0f0f0,
		0x9a9a9a, 0xfc6b83, 0x70b489, 0xf1b467, 0x87a6c4, 0xb48ead, 0x88c0d0, 0xffffff,
	})
}

func (colors palette) withCodeAndTerminal(name string, dark bool, text string, terminalBackground native.Color, syntax chroma.StyleEntries, ansi [16]uint32) palette {
	editor := editorPalette{
		background: colors.background, text: native.Hex(text), muted: colors.textMuted,
		fill: colors.accent, fillText: colors.accentText,
		border: colors.border, accent: colors.accent, added: colors.status.added, deleted: colors.status.danger,
	}
	editor.selection = colors.selection.Over(editor.background)
	editor.addedBackground = editor.added.Alpha(.12).Over(editor.background)
	editor.deletedBackground = editor.deleted.Alpha(.12).Over(editor.background)
	syntax[chroma.Text] = text
	syntax[chroma.KeywordType] = syntax[chroma.NameClass]
	syntax[chroma.GenericSubheading] = syntax[chroma.GenericHeading]
	syntax[chroma.GenericEmph] = syntax[chroma.Keyword]
	syntax[chroma.GenericStrong] = syntax[chroma.Keyword]
	syntax[chroma.GenericInserted] = fmt.Sprintf("#%02x%02x%02x", editor.added.R, editor.added.G, editor.added.B)
	syntax[chroma.GenericDeleted] = fmt.Sprintf("#%02x%02x%02x", editor.deleted.R, editor.deleted.G, editor.deleted.B)
	syntax[chroma.Error] = syntax[chroma.GenericDeleted]
	for token, hex := range syntax {
		color := legible(native.Hex(hex), dark, editor.background, editor.selection, editor.addedBackground, editor.deletedBackground, colors.surface)
		syntax[token] = fmt.Sprintf("#%02x%02x%02x", color.R, color.G, color.B)
	}
	editor.style = chroma.MustNewStyle(name, syntax)
	editor.codeColors = editor.markdownCodeColors()
	colors.editor = editor
	colors.term = terminal.Theme{
		Background: terminalBackground, Foreground: colors.text, Cursor: colors.accent,
		Selection: colors.selection, Search: colors.search.match, SearchSelected: colors.search.current,
		ANSI: ansi,
	}
	return colors
}
