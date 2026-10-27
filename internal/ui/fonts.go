package ui

import (
	_ "embed"
	"fmt"

	native "github.com/egoist/mygo/ui"
)

// Keep macOS UI Chinese glyphs ahead of the generic font cascade.
const (
	latinFont = "M PLUS Code Latin"
	textFont  = latinFont + ", .PingFang UI SC, system-ui"
	codeFont  = latinFont + ", .PingFang UI SC, monospace"
)

//go:embed fonts/MPLUSCodeLatin.ttf
var latinFontData []byte

func init() {
	if err := native.RegisterFont(latinFontData, latinFont); err != nil {
		panic(fmt.Errorf("register bundled %s font: %w", latinFont, err))
	}
}

func applyFontTheme(c *native.Context) {
	theme := *c.Theme()
	theme.Font = textFont
	c.SetTheme(&theme)
}
