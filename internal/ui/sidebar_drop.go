package ui

import native "github.com/egoist/mygo/ui"

func (a *App) receiveSidebarDrop(c *native.Context, list native.Element) {
	if paths := list.DroppedFiles(); len(paths) > 0 {
		a.addProjectPaths(paths)
	}
	if !list.FileDragOver() {
		return
	}
	theme := c.Theme()
	const hint = "拖入文件夹以添加项目"
	const size = float32(12)
	width, height := c.MeasureText(0, native.Span{Text: hint, Size: size})
	list.DrawOver(func(p *native.Painter, r native.Rect) {
		p.Stroke(r, theme.Accent, 0, 2)
		badge := native.Rect{
			X: r.X + (r.W-width)/2 - 8, Y: r.Y + 12,
			W: width + 16, H: height + 12,
		}
		p.Fill(badge, theme.Surface, 6)
		p.Text(badge.X+8, badge.Y+6, hint, size, theme.Accent)
	})
}
