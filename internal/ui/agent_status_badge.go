package ui

import (
	"github.com/shenming0821/Sasa/internal/agent"

	native "github.com/egoist/mygo/ui"
)

// agentBadge mirrors Mayoi's termIcon: a small dot at the icon's lower
// right corner, cut out of the icon by a ring of the surface under it.
// The color follows the session's state: waiting for the user, failed,
// working, or no dot at all when there's nothing to say.
func agentBadge(c *native.Context, kind agent.AgentKind, size float32, bg native.Color, status *agentStatusDot) native.Element {
	glyph, color := codexAgentIcon, c.Theme().Text
	switch kind.Normalized() {
	case agent.Claude:
		glyph, color = claudeAgentIcon, native.Hex("#D97757")
	}
	box := native.Box(c).Size(size, size).Shrink(0)
	box.Children(func() {
		native.Icon(c, glyph).Size(size, size).TextColor(color)
		if status == nil {
			return
		}
		const d, cut = float32(6), float32(1.5)
		ring := native.Box(c.Key("dot")).Absolute().Right(-2-cut).Bottom(-1-cut).Size(d+2*cut, d+2*cut).Radius((d + 2*cut) / 2).Background(bg)
		ring.Children(func() {
			native.Box(c).Size(d, d).Radius(d / 2).Background(status.color)
		})
	})
	return box
}

// agentStatusDot is what the dot says about a session.
type agentStatusDot struct {
	color native.Color
}

// sessionStatusDot returns the dot of a session, or nil for none: waiting
// for the user in the warning color, failed in danger, working in accent.
func sessionStatusDot(c *native.Context, s *session) *agentStatusDot {
	t := c.Theme()
	switch {
	case s == nil:
		return nil
	case len(s.State.PendingInteractions()) > 0:
		return &agentStatusDot{color: t.Warning}
	case s.State.Error != "":
		return &agentStatusDot{color: t.Danger}
	case s.State.Busy:
		return &agentStatusDot{color: t.Accent}
	}
	return nil
}
