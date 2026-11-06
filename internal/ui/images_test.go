package ui

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/transfer"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/attachment"
	"github.com/shenming0821/Fuyu/internal/codex"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func imageFixture(t *testing.T) (attachment.Image, []byte) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 480, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 480; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x / 2), uint8(y), 160, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "示例图片.png")
	if err := os.WriteFile(path, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return attachment.Image{Path: path, Name: "示例图片.png"}, b.Bytes()
}

func imageStore(t *testing.T, a *App) *storage.Store {
	t.Helper()
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := a.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestImagePasteDropAndSessionDraftRecovery(t *testing.T) {
	ref, raw := imageFixture(t)
	a := New(stubBackend{}, "test", "/first", nil)
	store := imageStore(t, a)
	queue := make(chan func(), 10)
	a.dispatch = func(fn func()) { queue <- fn }
	owner := a.session
	a.State.Draft = "请分析这张图片"
	tt := native.NewTester(func(c *native.Context) { native.Column(c).Fill().Children(func() { a.composer(c) }) }, 640, 360)
	a.readImageClipboard = func() (transfer.Data, error) { return tt.ClipboardData(), nil }
	if err := tt.Click("任务输入"); err != nil {
		t.Fatal(err)
	}
	tt.SetClipboard(" 保留文字")
	tt.Key(native.Cmd, native.KeyV)
	if a.State.Draft != "请分析这张图片 保留文字" {
		t.Fatal("ordinary text paste changed", a.State.Draft)
	}
	tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, raw))))
	tt.Key(native.Cmd, native.KeyV)
	if !a.imagesImporting || a.canSend() {
		t.Fatal("image import was not gated")
	}
	a.newSession("/second")
	pumpUpdate(t, queue)
	if len(owner.State.DraftImages) != 1 || len(a.State.DraftImages) != 0 {
		t.Fatal("paste changed the selected session instead of owner")
	}
	a.selectSession(owner)
	a.dispatch = nil
	tt.Frame()
	if !tt.HasText("预览附件 1") {
		t.Fatal("attachment preview missing")
	}
	a.dispatch = func(fn func()) { queue <- fn }
	input, _ := tt.Find("任务输入")
	if !tt.DropFiles(input.X+10, input.Y+10, ref.Path) {
		t.Fatal("file drop was not accepted")
	}
	pumpUpdate(t, queue)
	if len(a.State.DraftImages) != 1 {
		t.Fatal("same-content file drop duplicated screenshot")
	}
	a.dispatch = nil
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ref.Path); err != nil {
		t.Fatal(err)
	}
	reopened := New(nil, "test", "", nil)
	if err := reopened.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if len(reopened.State.DraftImages) != 1 || reopened.State.Draft != owner.State.Draft {
		t.Fatal("image draft did not survive restart")
	}
	if _, err := attachment.Read(reopened.State.DraftImages[0]); err != nil {
		t.Fatal("owned screenshot missing", err)
	}
}

func TestImagePreviewZoomRemoveAndMissingFileLayouts(t *testing.T) {
	ref, _ := imageFixture(t)
	for _, width := range []int{320, 640, 1000} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%v", width, dark), func(t *testing.T) {
				a := New(stubBackend{}, "test", "/workspace", nil)
				a.sidebarOpen = false
				a.State.DraftImages = []attachment.Image{ref, ref}
				a.State.Messages = []*app.Message{{ID: "image-message", Role: "user", Images: []attachment.Image{ref}, Text: "请分析截图里的布局 English 123", Done: true}}
				tt := native.NewTester(a.view, width, 760)
				tt.SetDark(dark)
				for _, name := range []string{"预览附件 1", "查看图片 image-message 1", "任务输入"} {
					r, ok := tt.Find(name)
					if !ok || r.X < 0 || r.X+r.W > float32(width)+.5 || r.Y+r.H > 760 {
						t.Fatalf("image control clipped: %s %+v", name, r)
					}
				}
				if err := tt.Click("查看图片 image-message 1"); err != nil {
					t.Fatal(err)
				}
				if !a.viewerOpen || !tt.HasText("关闭图片预览") {
					t.Fatal("viewer did not open")
				}
				if err := tt.Click("放大图片"); err != nil {
					t.Fatal(err)
				}
				if a.viewerZoom <= 1 {
					t.Fatal("zoom did not change")
				}
				if err := tt.Click("适应窗口"); err != nil {
					t.Fatal(err)
				}
				if a.viewerZoom != 1 {
					t.Fatal("fit did not reset zoom")
				}
				tt.Key(0, native.KeyEscape)
				if a.viewerOpen {
					t.Fatal("Escape did not dismiss viewer")
				}
				tt.Move(float32(width-1), 0)
				if _, ok := tt.Find("移除附件 1"); ok {
					t.Fatal("remove control visible before hover")
				}
				tile, _ := tt.Find("预览附件 1")
				if tile.W != tile.H || tile.W != 100 {
					t.Fatal("draft thumbnail is not square", tile)
				}
				tt.Move(tile.X+tile.W/2, tile.Y+tile.H/2)
				remove, ok := tt.Find("移除附件 1")
				if !ok || remove.X < tile.X+tile.W-28 || remove.Y > tile.Y || remove.Y < 0 {
					t.Fatal("hover remove is not at the top right", tile, remove)
				}
				if after, _ := tt.Find("预览附件 1"); after != tile {
					t.Fatal("hover changed attachment geometry", tile, after)
				}
				tt.Move(float32(width-1), 0)
				if _, ok := tt.Find("移除附件 1"); ok {
					t.Fatal("remove remained visible after leaving thumbnail")
				}
				tt.Move(tile.X+tile.W/2, tile.Y+tile.H/2)
				tt.Move(remove.X+remove.W/2, remove.Y+remove.H/2)
				if _, ok := tt.Find("移除附件 1"); !ok {
					t.Fatal("remove disappeared while entering its hit target")
				}
				if err := tt.Click("移除附件 1"); err != nil {
					t.Fatal(err)
				}
				if a.viewerOpen {
					t.Fatal("removing attachment also opened preview")
				}
				if len(a.State.DraftImages) != 1 || tt.HasText("移除附件 2") {
					t.Fatal("single attachment remove failed")
				}
				if output := os.Getenv("FUYU_IMAGES_SCREENSHOT"); output != "" && width == 1000 && !dark {
					f, err := os.Create(output)
					if err != nil {
						t.Fatal(err)
					}
					if err := png.Encode(f, tt.Image()); err != nil {
						t.Fatal(err)
					}
					f.Close()
				}
				a.State.Messages[0].Images = []attachment.Image{{Path: filepath.Join(t.TempDir(), "missing.png")}}
				tt.Frame()
				if !tt.HasText("图片不可用") {
					t.Fatal("missing image has no explanation")
				}
			})
		}
	}
}

type imageCaptureBackend struct {
	stubBackend
	sent chan []attachment.Image
	err  error
}

func (b *imageCaptureBackend) SendWithImages(_ context.Context, _, _ string, _ []codex.Skill, images []attachment.Image) (codex.Turn, error) {
	b.sent <- images
	return codex.Turn{ID: "image-turn"}, b.err
}

func TestImageOnlySendAndFailureRetainsAttachments(t *testing.T) {
	ref, _ := imageFixture(t)
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			b := &imageCaptureBackend{sent: make(chan []attachment.Image, 1)}
			if failed {
				b.err = errors.New("connection lost")
			}
			a := New(b, "test", "/workspace", nil)
			a.State.ThreadID = "native"
			a.State.DraftImages = []attachment.Image{ref}
			queue := make(chan func(), 4)
			a.dispatch = func(fn func()) { queue <- fn }
			if !a.canSend() {
				t.Fatal("image-only message cannot send")
			}
			a.send()
			pumpUpdate(t, queue)
			images := <-b.sent
			if len(images) != 1 || images[0].Path != ref.Path || len(a.State.Messages[0].Images) != 1 {
				t.Fatal("image was not sent")
			}
			if failed {
				if len(a.State.DraftImages) != 1 || !a.State.Disconnected {
					t.Fatal("uncertain submission lost attachments")
				}
			} else if len(a.State.DraftImages) != 0 || a.State.TurnID != "image-turn" {
				t.Fatal("successful send retained draft or lost turn")
			}
		})
	}
	a := New(stubBackend{}, "test", "/workspace", nil)
	a.State.DraftImages = []attachment.Image{ref}
	a.send()
	if len(a.State.Messages) != 0 || len(a.State.DraftImages) != 1 || a.State.Error == "" {
		t.Fatal("unsupported backend silently discarded image")
	}
	b := &imageCaptureBackend{sent: make(chan []attachment.Image, 1)}
	a = New(b, "test", "/workspace", nil)
	a.State.DraftImages = []attachment.Image{{Path: "/missing/file.png"}}
	a.send()
	if a.State.Busy || len(a.State.Messages) != 0 || a.State.Error == "" {
		t.Fatal("missing image submitted")
	}
}

func TestImageHistoryMaterializesDataURLs(t *testing.T) {
	_, raw := imageFixture(t)
	a := New(nil, "test", "/workspace", nil)
	imageStore(t, a)
	item, _ := json.Marshal(map[string]any{"type": "userMessage", "id": "user", "content": []any{map[string]any{"type": "image", "url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)}, map[string]any{"type": "text", "text": "请分析"}}})
	page := codex.TurnsPage{Data: []codex.HistoryTurn{{ID: "turn", Status: "completed", Items: []json.RawMessage{item}}}}
	a.materializeHistoryImages(&page)
	state, _, err := app.History("/workspace", "thread", page.Data)
	if err != nil || len(state.Messages[0].Images) != 1 {
		t.Fatal(err)
	}
	ref := state.Messages[0].Images[0]
	if ref.URL != "" || ref.Path == "" || state.Messages[0].Text != "请分析" {
		t.Fatal("history did not compact image", ref)
	}
	if _, err := attachment.Read(ref); err != nil {
		t.Fatal("history image unavailable", err)
	}
}

func TestImageQueuedImportsKeepDraftAndEnforceLimits(t *testing.T) {
	ref, _ := imageFixture(t)
	a := New(nil, "test", "/workspace", nil)
	imageStore(t, a)
	queue := make(chan func(), 8)
	a.dispatch = func(fn func()) { queue <- fn }
	a.addImages([]attachment.Image{ref})
	a.addImages([]attachment.Image{ref})
	if len(a.queuedImages) != 1 {
		t.Fatal("second paste was lost while first was importing")
	}
	a.addImages(make([]attachment.Image, attachment.MaxImages))
	if a.imageError == "" || len(a.queuedImages) != 1 {
		t.Fatal("attachment count limit not enforced")
	}
	pumpUpdate(t, queue)
	pumpUpdate(t, queue)
	if len(a.State.DraftImages) != 1 || a.imagesImporting || len(a.queuedImages) != 0 {
		t.Fatal("queued imports did not finish or deduplicate")
	}
	before := a.State.DraftImages[0]
	a.addImages([]attachment.Image{ref, {Path: "/missing/corrupt.png"}})
	pumpUpdate(t, queue)
	if len(a.State.DraftImages) != 1 || a.State.DraftImages[0] != before || a.imageError == "" {
		t.Fatal("failed batch changed existing image draft")
	}
}

func TestFileDragDataOffersAbsolutePaths(t *testing.T) {
	data := fileDragData([]string{"/workspace/main.go", "/other dir/说明.md"})
	files, err := data.Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0] != "/workspace/main.go" || files[1] != "/other dir/说明.md" {
		t.Fatalf("drag data did not round-trip file paths: %v", files)
	}
}

func TestDropAndPasteNonImageFilesInsertAtPrefixedAbsolutePaths(t *testing.T) {
	ref, _ := imageFixture(t)
	a := New(stubBackend{}, "test", "/workspace", nil)
	imageStore(t, a)
	queue := make(chan func(), 10)
	a.dispatch = func(fn func()) { queue <- fn }
	tt := native.NewTester(func(c *native.Context) { native.Column(c).Fill().Children(func() { a.composer(c) }) }, 640, 360)
	a.readImageClipboard = func() (transfer.Data, error) { return tt.ClipboardData(), nil }
	input, ok := tt.Find("任务输入")
	if !ok {
		t.Fatal("missing composer")
	}
	if !tt.DropFiles(input.X+10, input.Y+10, "/workspace/internal/ui/images.go", "/other/repo/README.md") {
		t.Fatal("non-image file drop was not accepted")
	}
	want := "@/workspace/internal/ui/images.go @/other/repo/README.md "
	if a.State.Draft != want {
		t.Fatalf("unexpected draft after file drop: got %q, want %q", a.State.Draft, want)
	}
	if _, ok := tt.Find("输入候选"); ok {
		t.Fatal("dropping files unexpectedly opened completion popup")
	}
	tt.Type("请检查")
	if a.State.Draft != want+"请检查" {
		t.Fatalf("typing after drop did not append at end: got %q", a.State.Draft)
	}
	fileData, err := transfer.FileData(ref.Path, "/workspace/cmd/desktop/main.go")
	if err != nil {
		t.Fatal(err)
	}
	tt.SetClipboardData(fileData)
	tt.Key(native.Cmd, native.KeyV)
	pumpUpdate(t, queue)
	want = want + "请检查 @/workspace/cmd/desktop/main.go "
	if a.State.Draft != want {
		t.Fatalf("mixed file paste did not append non-image path: got %q, want %q", a.State.Draft, want)
	}
	if len(a.State.DraftImages) != 1 || a.imageError != "" {
		t.Fatalf("mixed file paste failed image import: len=%d err=%q", len(a.State.DraftImages), a.imageError)
	}
}
