package ui

import (
	"github.com/alecthomas/chroma/v2"
	native "github.com/egoist/mygo/ui"
)

const marsThemeID = "mars"

func marsLightPalette() palette {
	colors := palette{
		background: native.Hex("#fdfdfe"), surface: native.Hex("#eef0f7"),
		surfaceHover: native.Hex("#e7eaf4"), surfacePressed: native.Hex("#dfe5f9"),
		border: native.Hex("#ced2e1"), text: native.Hex("#282b3c"), textMuted: native.Hex("#62677e"),
		accent: native.Hex("#5065a5"), accentText: native.Hex("#ffffff"),
		accentHover: native.Hex("#485c99"), accentPressed: native.Hex("#3c4e85"),
		selection: native.RGBA(80, 101, 165, .20), focus: native.RGBA(80, 101, 165, .65),
		search:         searchPalette{native.RGBA(187, 117, 156, .23), native.RGBA(187, 117, 156, .42), native.Hex("#875875")},
		pickerSelected: native.Hex("#dfe5f9"),
		status:         statusPalette{accent: native.Hex("#5065a5"), modified: native.Hex("#895f2e"), danger: native.Hex("#a43d54"), added: native.Hex("#397052")},
		chat:           paletteChat{userBubble: native.Hex("#f3e8ef"), composerCard: native.Hex("#eef0f7")},
	}
	return colors.withCodeAndTerminal("mars-light", false, "#222330", colors.background, chroma.StyleEntries{
		chroma.Keyword: "#8e1749", chroma.NameFunction: "#004b9b", chroma.NameClass: "#64359b",
		chroma.LiteralString: "#00603b", chroma.LiteralNumber: "#854000", chroma.Comment: "#555963",
		chroma.Operator: "#37323e", chroma.Punctuation: "#434653",
		chroma.NameConstant: "#854000", chroma.NameBuiltin: "#64359b", chroma.NameTag: "#8e1749",
		chroma.NameAttribute: "#15545d", chroma.GenericHeading: "#004b9b",
	}, [16]uint32{
		0x282b3c, 0xa43d54, 0x397052, 0x895f2e, 0x5065a5, 0x89546e, 0x366e78, 0xb5b9ca,
		0x62677e, 0xad4865, 0x40775e, 0x946833, 0x6376b2, 0x926381, 0x407784, 0xe2e5ef,
	})
}

func marsDarkPalette() palette {
	colors := palette{
		background: native.Hex("#19161b"), surface: native.Hex("#231f25"),
		surfaceHover: native.Hex("#32282f"), surfacePressed: native.Hex("#43252d"),
		border: native.Hex("#51444c"), text: native.Hex("#f3eced"), textMuted: native.Hex("#bbadb3"),
		accent: native.Hex("#ee7471"), accentText: native.Hex("#23161a"),
		accentHover: native.Hex("#fa8984"), accentPressed: native.Hex("#dc6969"),
		selection: native.RGBA(226, 109, 121, .24), focus: native.RGBA(238, 116, 113, .65),
		search:         searchPalette{native.RGBA(232, 175, 115, .22), native.RGBA(232, 175, 115, .40), native.Hex("#e8af73")},
		pickerSelected: native.Hex("#43252d"),
		status:         statusPalette{accent: native.Hex("#ee7471"), modified: native.Hex("#e8af73"), danger: native.Hex("#ff92b1"), added: native.Hex("#8bc5a1")},
		chat:           paletteChat{userBubble: native.Hex("#322a31"), composerCard: native.Hex("#231f25")},
	}
	return colors.withCodeAndTerminal("mars-dark", true, "#f3eced", colors.background, chroma.StyleEntries{
		chroma.Keyword: "#ee8b85", chroma.NameFunction: "#b7c6eb", chroma.NameClass: "#d6b8e1",
		chroma.LiteralString: "#e3a9bb", chroma.LiteralNumber: "#e8b684", chroma.Comment: "#bbadb3",
		chroma.Operator: "#d7cad0", chroma.Punctuation: "#bdafb6",
		chroma.NameConstant: "#e8b684", chroma.NameBuiltin: "#d6b8e1", chroma.NameTag: "#ee8b85",
		chroma.NameAttribute: "#b7c6eb", chroma.GenericHeading: "#ee8b85",
	}, [16]uint32{
		0x322a31, 0xee7471, 0x8bc5a1, 0xe8af73, 0x9cafd5, 0xd699b4, 0x87bfbe, 0xd6cbd1,
		0xa79aa3, 0xff92a1, 0xa4d7b6, 0xf3c893, 0xb7c6eb, 0xeab7ce, 0xa5d6d4, 0xf3eced,
	})
}
