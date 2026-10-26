package ui

import (
	"sort"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/workspace"
)

func (a *App) observeFile(d *filePage) {
	if d.buffer == nil {
		return
	}
	total := 0
	for _, other := range a.files {
		if other != d && other.dirty() {
			total += other.buffer.ByteLen()
		}
	}
	if d.buffer.Version() != d.observed.Version() && d.buffer.ByteLen()+total > 64<<20 {
		d.buffer.Restore(d.observed)
		d.observed = d.buffer.Snapshot()
		d.err = "未保存内容已达 64 MiB，已拒绝本次编辑；请先保存或关闭其他文件"
		d.resync = true
		return
	}
	d.observe()
}
func (a *App) trimEditorCaches() {
	total, history, colors := 0, 0, 0
	var hidden, cache []*filePage
	for _, d := range a.files {
		if d.buffer != nil {
			total += d.buffer.ByteLen()
		}
		for _, s := range d.undo {
			history += s.ByteLen()
		}
		for _, s := range d.redo {
			history += s.ByteLen()
		}
		colors += len(d.highlights) * 32
		if d.id != a.activeTab && d.loaded && !d.highlighting {
			cache = append(cache, d)
		}
		if d.id != a.activeTab && !d.dirty() && !d.saving && !d.checking && !d.highlighting && d.loaded && !d.loading {
			hidden = append(hidden, d)
		}
	}
	sort.Slice(cache, func(i, j int) bool { return cache[i].lastVisited.Before(cache[j].lastVisited) })
	sort.Slice(hidden, func(i, j int) bool { return hidden[i].lastVisited.Before(hidden[j].lastVisited) })
	for _, d := range cache {
		if colors > 16<<20 {
			colors -= len(d.highlights) * 32
			d.highlights = nil
			d.highlightVersion = 0
		}
		if history > 32<<20 {
			for _, s := range d.undo {
				history -= s.ByteLen()
			}
			for _, s := range d.redo {
				history -= s.ByteLen()
			}
			d.undo = nil
			d.redo = nil
		}
	}
	for _, d := range hidden {
		if total > 64<<20 {
			total -= d.buffer.ByteLen()
			d.buffer = nil
			d.loaded = false
			d.highlights = nil
			d.file.Text = ""
			d.savedText = ""
			d.observed = native.TextSnapshot{}
			d.undo = nil
			d.redo = nil
		}
	}
	// Release watchers once neither the selected workspace nor an open file uses them.
	for root, w := range a.workspaces {
		used := root == a.activeWorkspace
		for _, d := range a.files {
			used = used || d.root == root
		}
		if !used && w.watcher != nil {
			w.cancel()
			w.watcher.Close()
			w.watcher = nil
			w.children = map[string][]workspace.Entry{}
		}
	}
}
