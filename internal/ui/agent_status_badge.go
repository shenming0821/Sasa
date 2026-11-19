package ui

import (
	"math"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"

	native "github.com/egoist/mygo/ui"
)

// agentBadge mirrors Mayoi's termIcon: a small dot at the icon's lower
// right corner, cut out of the icon by a ring of the surface under it,
// that pops in and out with dotPop and eases between colors, sends out
// rings while the session waits for the user, and once as it becomes
// ready, all paused for less motion.
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
		badge := native.Box(c.Key("dot")).Absolute().Right(-2-cut).Bottom(-1-cut).Size(d+2*cut, d+2*cut).Transition(dotPop)
		col := badge.AnimateColor("col", status.color, native.Smooth)
		bg = badge.AnimateColor("bg", bg, native.Animation{Duration: 200 * time.Millisecond})
		calm := c.Preferences().ReduceMotion
		var pulse time.Time
		if !calm && status.pending && time.Since(status.since) < badgePulseFor {
			pulse = status.since
		}
		badge.Draw(func(pt *native.Painter, r native.Rect) {
			drawStatusDot(pt, r, cut, col, bg, pulse)
		})
	})
	return box
}

// dotPop is how a small mark comes and goes, as the badge of an agent
// icon: it grows from half its size and shrinks back. Mirrors Mayoi.
var dotPop = native.ElementTransition{
	Animation: &native.Snappy,
	Enter:     &native.Motion{Scale: 0.5},
	Exit:      &native.Motion{Scale: 0.5},
}

// badgePulseBeat is how often the badge of a session that waits for the
// user sends out a ring. Mirrors Mayoi's pulseBeat.
const badgePulseBeat = 1400 * time.Millisecond

// badgePulseFor is how long a session's badge sends out rings as it
// begins to wait, whole beats of them: after, it stays still. Mirrors
// Mayoi's pulseFor.
const badgePulseFor = 6 * badgePulseBeat

// agentStatusDot is what the dot says about a session.
type agentStatusDot struct {
	color   native.Color
	pending bool
	since   time.Time
}

// sessionStatusDot returns the dot of a session, or nil for none: waiting
// for the user in the warning color, failed in danger, working in accent.
// pending marks whether the session blocks on the user, so its badge may
// send out rings for badgePulseFor from since.
func sessionStatusDot(c *native.Context, s *session) *agentStatusDot {
	t := c.Theme()
	switch {
	case s == nil:
		return nil
	case len(s.State.PendingInteractions()) > 0:
		return &agentStatusDot{color: t.Warning, pending: true, since: s.pendingSince()}
	case s.State.Error != "":
		return &agentStatusDot{color: t.Danger}
	case s.State.Busy:
		return &agentStatusDot{color: t.Accent}
	}
	return nil
}

// pendingSince is the time the session first waited on its pending
// interactions; it stays zero while none waits. A zero time is never
// sent to the dot.
func (s *session) pendingSince() time.Time {
	if len(s.State.PendingInteractions()) == 0 {
		s.badgePendingSince = time.Time{}
		return time.Time{}
	}
	if s.badgePendingSince.IsZero() {
		s.badgePendingSince = time.Now()
	}
	return s.badgePendingSince
}

// drawStatusDot paints a dot of col in r, inside a ring of bg cut wide.
// For badgePulseFor from pulse, unless it is zero, rings swell from the
// dot and fade, as a beacon, at 30 frames a second. Mirrors Mayoi's
// statusDot.
func drawStatusDot(pt *native.Painter, r native.Rect, cut float32, col, bg native.Color, pulse time.Time) {
	dot := native.Rect{X: r.X + cut, Y: r.Y + cut, W: r.W - 2*cut, H: r.H - 2*cut}
	drawBeacon(pt, dot, col, pulse, 0.9, 0.45)
	pt.Fill(r, bg, r.W/2)
	pt.Fill(dot, col, dot.W/2)
}

// drawBeacon paints, for badgePulseFor from pulse unless it is zero, a
// ring of col that swells from dot and fades every badgePulseBeat, at 30
// frames a second, and reports whether it did. Mirrors Mayoi's beacon.
func drawBeacon(pt *native.Painter, dot native.Rect, col native.Color, pulse time.Time, reach, alpha float32) bool {
	waited := pt.Now().Sub(pulse)
	if pulse.IsZero() || waited >= badgePulseFor {
		return false
	}
	drawRing(pt, dot, col, float32(waited%badgePulseBeat)/float32(badgePulseBeat), reach, alpha)
	pt.AnimationFrame()
	return true
}

// drawRing paints a ring of col swollen from dot by reach of its width,
// t of the way, fading as it goes. Mirrors Mayoi's ring.
func drawRing(pt *native.Painter, dot native.Rect, col native.Color, t, reach, alpha float32) {
	grow := dot.W * reach * easeOutCubic(t)
	o := native.Rect{X: dot.X - grow, Y: dot.Y - grow, W: dot.W + 2*grow, H: dot.H + 2*grow}
	pt.Fill(o, col.Alpha(alpha*(1-t)), o.W/2)
}

func easeOutCubic(t float32) float32 {
	return 1 - float32(math.Pow(float64(1-t), 3))
}
