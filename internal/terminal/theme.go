package terminal

import (
	"fmt"
	"strings"

	native "github.com/egoist/mygo/ui"
)

type Theme struct {
	Background, Foreground, Cursor    native.Color
	Selection, Search, SearchSelected native.Color
	ANSI                              [16]uint32
}

func (theme Theme) config() string {
	if theme == (Theme{}) {
		return ""
	}
	var config strings.Builder
	for _, entry := range []struct {
		key   string
		color native.Color
	}{
		{"background", theme.Background},
		{"foreground", theme.Foreground},
		{"cursor-color", theme.Cursor},
		{"selection-background", theme.Selection.Over(theme.Background)},
		{"selection-foreground", theme.Foreground},
		{"search-background", theme.Search.Over(theme.Background)},
		{"search-foreground", theme.Foreground},
		{"search-selected-background", theme.SearchSelected.Over(theme.Background)},
		{"search-selected-foreground", theme.Foreground},
	} {
		fmt.Fprintf(&config, "%s = #%02x%02x%02x\n", entry.key, entry.color.R, entry.color.G, entry.color.B)
	}
	for index, color := range theme.ANSI {
		fmt.Fprintf(&config, "palette = %d=#%06x\n", index, color)
	}
	return config.String()
}
