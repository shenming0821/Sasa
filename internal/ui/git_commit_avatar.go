// Portions adapted from Mayoi, Copyright (c) 2026 chencheng.
// Licensed under MIT; see THIRD_PARTY_NOTICES/Mayoi-LICENSE.
package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Sasa/internal/git"
)

// These local marks and co-author identities follow Mayoi's agent catalog.
// No avatar service or network request is involved.
var gitCoAuthorMarks = map[string]*native.SVG{
	"claude": icon(`<path d="M12 3v18M3 12h18M6.7 6.7l10.6 10.6M17.3 6.7 6.7 17.3"/>`),
	"codex":  icon(`<path d="M12 2.5 20.5 7.25v9.5L12 21.5 3.5 16.75v-9.5Z"/><path d="m10 8.5 3.5 3.5-3.5 3.5"/>`),
	"qoder":  icon(`<circle cx="11.5" cy="11" r="8"/><path d="m13.5 14.5 6 6.5"/>`),
	"grok":   icon(`<path d="M17.5 6.2A8 8 0 1 0 19.8 13M20.5 3.5 9 15"/>`),
	"pi":     icon(`<path d="M4.5 7h15M7.5 7v11M17.5 7c.5 5-1 11-5.5 11"/>`),
}

func gitCoAuthorMark(people []gitview.Person) string {
	for _, p := range people {
		email := strings.ToLower(p.Email)
		_, domain, _ := strings.Cut(email, "@")
		switch domain {
		case "anthropic.com":
			return "claude"
		case "openai.com":
			return "codex"
		case "qoder.com":
			return "qoder"
		case "x.ai":
			return "grok"
		}
		if email != "" && !strings.Contains(email, "noreply") {
			continue
		}
		name, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(p.Name)), " ")
		switch name {
		case "claude-code":
			name = "claude"
		case "qodercli":
			name = "qoder"
		}
		if gitCoAuthorMarks[name] != nil {
			return name
		}
	}
	return ""
}

func gitAuthorInitials(name string) string {
	var initials []rune
	for _, word := range strings.Fields(name) {
		r, _ := utf8.DecodeRuneInString(word)
		initials = append(initials, unicode.ToUpper(r))
		if len(initials) == 2 {
			break
		}
	}
	if len(initials) == 0 {
		return "?"
	}
	return string(initials)
}

func gitCommitAvatar(c *native.Context, cm gitview.CommitInfo, size float32, ring native.Color) {
	hash := uint32(2166136261)
	for _, b := range []byte(cm.Email + cm.Author) {
		hash = (hash ^ uint32(b)) * 16777619
	}
	t := c.Theme()
	color := commitGraphColor(t, int(hash%6))
	box := native.Box(c).Size(size, size).Shrink(0).Radius(size / 2).Background(color.Alpha(.22)).
		AlignItems(native.Center).Justify(native.Center)
	box.Children(func() {
		native.Text(c, gitAuthorInitials(cm.Author)).FontSize(size * .47).Bold().TextColor(color)
		if mark := gitCoAuthorMark(cm.CoAuthors); mark != "" {
			tint := t.TextMuted
			switch mark {
			case "claude":
				tint = native.Hex("#D97757")
			case "qoder":
				tint = native.Hex("#13985a")
			case "pi":
				tint = native.Hex("#6d5ce0")
			}
			d := float32(int(size*.64 + .5))
			native.Box(c).Absolute().Right(-d/3-1).Bottom(-d/3-1).Size(d+2, d+2).Radius((d + 2) / 2).
				Background(ring).AlignItems(native.Center).Justify(native.Center).Children(func() {
				native.Box(c).Size(d, d).Radius(d / 2).Background(tint).AlignItems(native.Center).Justify(native.Center).Children(func() {
					native.Icon(c, gitCoAuthorMarks[mark]).Size(d*.78, d*.78).TextColor(ring)
				})
			})
		}
	})
}
