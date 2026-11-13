package ui

import (
	"time"

	native "github.com/egoist/mygo/ui"
)

// disclosureAnimation is a critically damped spring fast enough to settle inside
// typical 250–300 ms gesture windows while preserving velocity when reversed mid-flight.
var disclosureAnimation = native.Animation{
	Spring: native.SpringOptions{
		Duration:  180 * time.Millisecond,
		Precision: 0.002,
	},
}

func animateDisclosure(e native.Element, key any, open bool) float32 {
	target := float32(0)
	if open {
		target = 1
	}
	return e.AnimateTo(key, target, disclosureAnimation)
}

// Handle input before sampling motion so the click frame starts the spring.
type collapsibleControl struct {
	Trigger  native.Element
	c        *native.Context
	open     *bool
	progress float32
	toggled  bool
}

func newCollapsible(c *native.Context, open *bool) collapsibleControl {
	tr := native.ButtonBase(c).Justify(native.Start).Role(native.RoleDisclosure).Expanded(*open).Shrink(0)
	toggled := false
	if tr.Clicked() {
		*open = !*open
		tr.Expanded(*open)
		toggled = true
	}
	progress := animateDisclosure(tr, "open", *open)
	return collapsibleControl{Trigger: tr, c: c, open: open, progress: progress, toggled: toggled}
}

func (p collapsibleControl) Progress() float32 { return p.progress }

func (p collapsibleControl) Moving() bool {
	target := float32(0)
	if *p.open {
		target = 1
	}
	return p.toggled || p.progress != target
}

func (p collapsibleControl) Panel(fn func()) native.Element {
	return disclosurePanel(p.c, "collapsible-panel", *p.open, p.progress, fn)
}

// Keep the clip in normal flow: following siblings move with its height.
// The unshrunk inner column retains its natural layout throughout the motion.
func disclosurePanel(c *native.Context, key any, open bool, progress float32, fn func()) native.Element {
	anchor := native.Column(c.Key(key)).FillWidth().MinWidth(0).Shrink(0)
	height := native.Local(anchor, "content-height", func() float32 { return 0 })
	if !open && progress <= 0 {
		return native.Element{}
	}
	var inner native.Element
	anchor.Children(func() {
		inner = native.Column(c.Key("content")).FillWidth().MinWidth(0).Shrink(0)
		inner.Children(fn)
	})
	if h := inner.LayoutBounds().H; h > 0 {
		*height = h
	}
	if progress < 1 {
		anchor.ClipY().Height(max(0, progress) * *height)
		if *height == 0 {
			c.AnimationFrame()
		}
	}
	return anchor
}

func collapsibleSection(c *native.Context, key any, open bool, fn func()) native.Element {
	var motion native.Element
	native.Column(c.Key(key)).FillWidth().MinWidth(0).Shrink(0).Children(func() {
		motion = native.Box(c.Key("motion")).Size(0, 0)
		disclosurePanel(c, "panel", open, animateDisclosure(motion, "open", open), fn)
	})
	return motion
}
