package ui

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/app"
	"github.com/shenming0821/Sasa/internal/attachment"
)

type imageSender interface {
	SendWithImages(context.Context, string, string, []agent.Skill, []attachment.Image) (agent.Turn, error)
}

type imagePreview struct {
	bitmap *native.Bitmap
	err    error
	used   uint64
}

var removeImageIcon = icon(`<path d="m6 6 12 12M18 6 6 18"/>`)

var photoIcon = icon(`<rect x="3" y="3" width="18" height="18" rx="3"/><circle cx="8" cy="8" r="1.5"/><path d="m3 17 5-5 4 4 4-6 5 7"/>`)

func (a *App) imageDirectory() (string, error) {
	if store := a.application().store; store != nil {
		return filepath.Join(store.Directory(), "attachments"), nil
	}
	return "", fmt.Errorf("图片存储尚未就绪")
}

func (a *App) addImages(refs []attachment.Image) {
	if len(refs) == 0 {
		return
	}
	if len(refs)+len(a.State.DraftImages)+a.importingImages+len(a.queuedImages) > attachment.MaxImages {
		a.imageError = "每条消息最多添加 8 张图片"
		a.focusComposer()
		return
	}
	if a.imagesImporting {
		a.queuedImages = append(a.queuedImages, refs...)
		return
	}
	dir, err := a.imageDirectory()
	if err != nil {
		a.imageError = err.Error()
		a.focusComposer()
		return
	}
	bound := a.forSession(a.session)
	bound.imagesImporting, bound.imageError = true, ""
	bound.importingImages = len(refs)
	go func() {
		var imported []attachment.Image
		var importErr error
		for _, ref := range refs {
			image, err := attachment.Import(dir, ref)
			if err != nil {
				importErr = fmt.Errorf("%s：%w", imageName(ref), err)
				break
			}
			imported = append(imported, image)
		}
		bound.update(func() {
			bound.imagesImporting = false
			bound.importingImages = 0
			if importErr != nil {
				bound.imageError = importErr.Error()
			} else {
				for _, image := range imported {
					if !slices.ContainsFunc(bound.State.DraftImages, func(old attachment.Image) bool { return old.Path == image.Path }) {
						bound.State.DraftImages = append(bound.State.DraftImages, image)
					}
				}
			}
			queued, previousError := bound.queuedImages, bound.imageError
			bound.queuedImages = nil
			bound.addImages(queued)
			if previousError != "" {
				bound.imageError = previousError
			}
			if bound.application().session == bound.session {
				bound.focusComposer()
			}
		})
	}()
}

// focusComposer moves the caret back to the composer so the user can keep
// typing or paste the next image right after adding one.
func (a *App) focusComposer() {
	a.completion.caret = len([]rune(a.State.Draft))
	a.completion.focus = true
}

func imageName(ref attachment.Image) string {
	if ref.Name != "" {
		return ref.Name
	}
	if ref.Path != "" {
		return filepath.Base(ref.Path)
	}
	return "图片"
}

func (a *App) imagePickerButton(c *native.Context) {
	button := quietIconButton(c, "添加图片").Size(28, 28).Disabled(a.imagesImporting)
	button.Children(func() { native.Icon(c, photoIcon).Size(18, 18) })
	if button.Clicked() {
		bound := a.forSession(a.session)
		go func() {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.application().win, Title: "添加图片", Multiple: true,
				Filters: []mygo.FileFilter{{Name: "图片", Extensions: []string{"png", "jpg", "jpeg", "gif", "webp", "bmp"}}}})
			bound.update(func() {
				if err != nil {
					bound.imageError = err.Error()
					return
				}
				var refs []attachment.Image
				for _, path := range paths {
					refs = append(refs, attachment.Image{Path: path})
				}
				bound.addImages(refs)
			})
		}()
	}
}

// fileDrag carries paths dragged within the window; DragData offers the same
// paths to other applications as file URLs.
type fileDrag struct{ paths []string }

func fileDragData(paths []string) transfer.Data {
	data, err := transfer.FileData(paths...)
	if err != nil {
		return transfer.Data{}
	}
	return data
}

func dragFiles(e native.Element, paths []string) {
	e.Drag(fileDrag{paths}).DragData(fileDragData(paths))
}

// filePathRanges finds the rune ranges of @path tokens, as file drops and
// @ completion insert them, for the composer to highlight.
func filePathRanges(text string) [][2]int {
	var ranges [][2]int
	r := []rune(text)
	for i := 0; i+1 < len(r); i++ {
		if r[i] != '@' || unicode.IsSpace(r[i+1]) || i > 0 && !unicode.IsSpace(r[i-1]) {
			continue
		}
		end := i + 2
		for end < len(r) && !unicode.IsSpace(r[end]) {
			end++
		}
		ranges = append(ranges, [2]int{i, end})
		i = end
	}
	return ranges
}

func isImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	default:
		return false
	}
}

func (a *App) addDroppedFiles(paths []string) {
	var refs []attachment.Image
	var files []string
	for _, path := range paths {
		if isImagePath(path) {
			refs = append(refs, attachment.Image{Path: path})
		} else if trimmed := strings.TrimSpace(path); trimmed != "" {
			files = append(files, "@"+filepath.Clean(trimmed))
		}
	}
	a.addImages(refs)
	if len(files) == 0 {
		return
	}
	if a.renderingPane != nil {
		a.focusPane(a.activeWorkspace, a.renderingPane)
	}
	prefix := a.State.Draft
	if prefix != "" {
		r := []rune(prefix)
		if !unicode.IsSpace(r[len(r)-1]) {
			prefix += " "
		}
	}
	a.State.Draft = prefix + strings.Join(files, " ") + " "
	a.completion.caret = len([]rune(a.State.Draft))
	a.completion.focus = true
}

func (a *App) takeImageData(data transfer.Data) bool {
	if files, err := data.Files(); err == nil && len(files) > 0 {
		a.addDroppedFiles(files)
		return true
	}
	var refs []attachment.Image
	for _, item := range data.Items() {
		if !slices.Contains(item.Formats(), transfer.PNG) {
			continue
		}
		raw, err := item.Read(transfer.PNG)
		if err != nil {
			a.imageError = "无法读取剪贴板图片：" + err.Error()
			return true
		}
		if len(raw) > attachment.MaxBytes {
			a.imageError = "每张图片需小于 10 MiB"
			return true
		}
		refs = append(refs, attachment.Image{Name: "截图.png", URL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)})
	}
	if len(refs) == 0 {
		return false
	}
	a.addImages(refs)
	return true
}

func (a *App) pasteImages() bool {
	read := a.application().readImageClipboard
	if read == nil {
		read = func() (transfer.Data, error) {
			return mygo.Clipboard.Read(transfer.FileList, transfer.URIList, transfer.PNG)
		}
	}
	data, err := read()
	if err != nil {
		return false
	}
	return a.takeImageData(data)
}

func (a *App) receiveImageDrop(box native.Element) {
	if paths := box.DroppedFiles(); len(paths) > 0 {
		a.addDroppedFiles(paths)
	}
	if drop, ok := native.DropData(box, transfer.DropOptions{Formats: []transfer.Format{transfer.FileList, transfer.URIList, transfer.PNG}}); ok {
		a.takeImageData(drop.Data)
	}
	if drag, ok := native.Drop[fileDrag](box); ok {
		a.addDroppedFiles(drag.paths)
	}
}

func (a *App) restoreSubmittedImages(text string) {
	if len(a.State.DraftImages) > 0 || a.State.Draft != text {
		return
	}
	for i := len(a.State.Messages) - 1; i >= 0; i-- {
		if m := a.State.Messages[i]; m.Role == "user" && m.Delivery != "queued" && m.Text == text {
			if m.Edited {
				return
			}
			a.State.DraftImages = slices.Clone(m.Images)
			return
		}
	}
}

func imageKey(ref attachment.Image) string {
	if ref.Path != "" {
		return ref.Path
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(ref.URL)))
}

func decodeImage(ref attachment.Image, full bool) *imagePreview {
	entry := &imagePreview{}
	data, err := attachment.Read(ref)
	if err == nil {
		if full {
			entry.bitmap, err = native.DecodeBitmap(data)
		} else {
			var img image.Image
			img, err = attachment.Thumbnail(data)
			if err == nil {
				entry.bitmap = native.NewBitmap(img)
			}
		}
	}
	entry.err = err
	return entry
}

func (a *App) preview(c *native.Context, ref attachment.Image) *imagePreview {
	return a.previewImage(c, ref, false)
}

func (a *App) previewImage(c *native.Context, ref attachment.Image, full bool) *imagePreview {
	root := a.application()
	if root.imageCache == nil {
		root.imageCache = make(map[string]*imagePreview)
	}
	key := imageKey(ref)
	if full {
		key = "full:" + key
	}
	root.imageClock++
	if entry := root.imageCache[key]; entry != nil {
		entry.used = root.imageClock
		return entry
	}
	if root.imageLoading {
		return nil
	}
	root.imageLoading = true
	install := func(entry *imagePreview) {
		// Bound decoded bitmaps; the original attachment files stay available.
		usedBytes := func() int64 {
			var n int64
			for _, e := range root.imageCache {
				if e.bitmap != nil {
					w, h := e.bitmap.Size()
					n += int64(w) * int64(h) * 8
				}
			}
			return n
		}
		if full {
			for k := range root.imageCache {
				if strings.HasPrefix(k, "full:") {
					delete(root.imageCache, k)
				}
			}
		}
		entry.used = root.imageClock
		root.imageCache[key] = entry
		for len(root.imageCache) > 1 && (len(root.imageCache) > 17 || usedBytes() > 256<<20) {
			oldest := ""
			for k, e := range root.imageCache {
				if k != key && (oldest == "" || e.used < root.imageCache[oldest].used) {
					oldest = k
				}
			}
			delete(root.imageCache, oldest)
		}
		root.imageLoading = false
	}
	if root.win == nil && root.dispatch == nil {
		install(decodeImage(ref, full))
		return root.imageCache[key]
	}
	go func() { entry := decodeImage(ref, full); root.update(func() { install(entry) }) }()
	return nil
}

func (a *App) imageTile(c *native.Context, ref attachment.Image, label string, width, height float32) {
	a.imageTileWithFit(c, ref, label, width, height, native.Contain)
}

func (a *App) imageTileWithFit(c *native.Context, ref attachment.Image, label string, width, height float32, fit native.Fit) native.Element {
	entry := a.preview(c, ref)
	button := native.ButtonBase(c).Label(label).Size(width, height).Radius(10).Clip().Background(c.Theme().Surface)
	if fit == native.Cover {
		button.Border(1, c.Theme().Border)
	}
	button.Children(func() {
		if entry == nil {
			native.Text(c, "加载图片…").FontSize(11).TextColor(c.Theme().TextMuted)
		} else if entry.err != nil {
			native.Column(c).Fill().Padding(8).Gap(4).Children(func() {
				native.Text(c, "图片不可用").FontSize(12).TextColor(c.Theme().TextMuted)
				native.Text(c, entry.err.Error()).FontSize(10).TextColor(c.Theme().TextMuted).MaxLines(3)
			})
		} else {
			native.Image(c, entry.bitmap).Fill().Fit(fit)
		}
	})
	if button.Clicked() {
		root := a.application()
		root.viewerImage, root.viewerOpen, root.viewerZoom = ref, true, 1
		root.viewerScroll = native.ScrollState{}
	}
	return button
}

func (a *App) draftImageStrip(c *native.Context) {
	removeIndex := -1
	if len(a.State.DraftImages) > 0 {
		native.ScrollHorizontal(c).FillWidth().Height(112).Children(func() {
			native.Row(c).Gap(8).Children(func() {
				for i, ref := range a.State.DraftImages {
					card := native.Box(c.Key(fmt.Sprint(i))).Size(112, 112).Shrink(0).Padding(6)
					card.Children(func() {
						tile := a.imageTileWithFit(c, ref, fmt.Sprintf("预览附件 %d", i+1), 100, 100, native.Cover)
						if card.Hovered() || card.FocusWithin() || tile.FocusVisible() {
							remove := native.ButtonBase(c.Key("remove")).Label(fmt.Sprintf("移除附件 %d", i+1)).
								Absolute().Right(-4).Top(-4).Size(28, 28).Padding(0).Radius(14).
								TextColor(native.RGBA(255, 255, 255, 1))
							remove.DrawOver(func(p *native.Painter, r native.Rect) {
								p.Stroke(native.Rect{X: r.X + 4, Y: r.Y + 4, W: r.W - 8, H: r.H - 8}, c.Theme().Background, 10, 1)
							})
							buttonFace(c, remove, native.RGBA(30, 33, 36, 1), native.RGBA(56, 59, 61, 1), native.RGBA(20, 23, 26, 1), 10, 4)
							remove.Children(func() { native.Icon(c, removeImageIcon).Size(10, 10) })
							if remove.Clicked() {
								removeIndex = i
							}
						}
					})
				}
			})
		})
	}
	if removeIndex >= 0 {
		a.State.DraftImages = slices.Delete(a.State.DraftImages, removeIndex, removeIndex+1)
		a.imageError = ""
	}
	if a.imagesImporting {
		native.Text(c, "正在添加图片…").FontSize(11).TextColor(c.Theme().TextMuted)
	}
	if a.imageError != "" {
		native.Text(c, a.imageError).FontSize(11).TextColor(c.Theme().Danger)
	}
}

func (a *App) messageImages(c *native.Context, m *app.Message) {
	native.Row(c).FillWidth().Justify(native.End).Children(func() {
		native.ScrollHorizontal(c).MaxWidthPercent(85).Width(float32(len(m.Images)) * 168).Height(124).Children(func() {
			native.Row(c).Gap(8).Children(func() {
				for i, ref := range m.Images {
					a.imageTile(c, ref, fmt.Sprintf("查看图片 %s %d", m.ID, i+1), 160, 120)
				}
			})
		})
	})
}

func (a *App) imageViewer(c *native.Context) {
	root := a.application()
	native.Modal(c.Key("image-viewer"), &root.viewerOpen, func() {
		w, h := c.Size()
		width, height := min(w-80, 1000), max(float32(100), h-160)
		native.Column(c).Width(width).Height(height).Gap(10).Children(func() {
			native.Row(c).FillWidth().AlignItems(native.Center).Gap(8).Children(func() {
				native.Text(c, imageName(root.viewerImage)).FontSize(13).Grow(1).MinWidth(0).SingleLine()
				if imageControl(c, "缩小图片", "−").Clicked() {
					root.viewerZoom = max(float32(.25), root.viewerZoom/1.25)
				}
				if imageControl(c, "适应窗口", fmt.Sprintf("%.0f%%", root.viewerZoom*100)).Clicked() {
					root.viewerZoom = 1
					root.viewerScroll = native.ScrollState{}
				}
				if imageControl(c, "放大图片", "+").Clicked() {
					root.viewerZoom = min(float32(8), root.viewerZoom*1.25)
				}
				if imageControl(c, "关闭图片预览", "×").Clicked() {
					root.viewerOpen = false
				}
			})
			native.ScrollBoth(c).Fill().TrackScroll(&root.viewerScroll).Children(func() {
				entry := a.previewImage(c, root.viewerImage, true)
				if entry == nil {
					native.Text(c, "加载图片…")
				} else if entry.err != nil {
					native.Text(c, entry.err.Error()).TextColor(c.Theme().TextMuted)
				} else {
					iw, ih := entry.bitmap.Size()
					scale := min(width/float32(iw), (height-42)/float32(ih)) * root.viewerZoom
					native.Image(c, entry.bitmap).Size(float32(iw)*scale, float32(ih)*scale).Shrink(0).Fit(native.Contain)
				}
			})
		})
	})
}

// Compact data URLs before projection so native history does not retain large
// base64 strings in the UI state. Remote URLs are never fetched automatically.
func (a *App) materializeHistoryImages(page *agent.TurnsPage) {
	dir, err := a.imageDirectory()
	if err != nil {
		return
	}
	for ti := range page.Data {
		for ii, raw := range page.Data[ti].Items {
			var item map[string]json.RawMessage
			if json.Unmarshal(raw, &item) != nil || string(item["type"]) != `"userMessage"` {
				continue
			}
			var content []map[string]json.RawMessage
			if json.Unmarshal(item["content"], &content) != nil {
				continue
			}
			changed := false
			for _, input := range content {
				var kind, url string
				json.Unmarshal(input["type"], &kind)
				json.Unmarshal(input["url"], &url)
				if kind != "image" && kind != "localImage" {
					continue
				}
				if !strings.HasPrefix(url, "data:image/") {
					continue
				}
				ref, err := attachment.Import(dir, attachment.Image{URL: url, Name: "历史图片"})
				if err != nil {
					continue
				}
				input["type"] = json.RawMessage(`"localImage"`)
				input["path"], _ = json.Marshal(ref.Path)
				delete(input, "url")
				changed = true
			}
			if changed {
				item["content"], _ = json.Marshal(content)
				page.Data[ti].Items[ii], _ = json.Marshal(item)
			}
		}
	}
}

func imageControl(c *native.Context, label, text string) native.Element {
	b := quietButton(c, label).MinWidth(28).Height(28).Padding(0, 6)
	b.Children(func() { native.Text(c, text).FontSize(13) })
	return b
}
