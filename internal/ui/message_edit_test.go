package ui

import (
	"context"
	"encoding/json"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/app"
	"github.com/shenming0821/Sasa/internal/attachment"
	"github.com/shenming0821/Sasa/internal/storage"
)

type editTestBackend struct {
	stubBackend
	result agent.HistoryEdit
	err    error
	edits  chan string
	sends  chan string
	stops  chan string
	skills chan []agent.Skill
}

func (backend *editTestBackend) SendWithSkills(ctx context.Context, thread, text string, skills []agent.Skill) (agent.Turn, error) {
	if backend.skills != nil {
		backend.skills <- skills
	}
	return backend.Send(ctx, thread, text)
}

func TestMessageEditPreservesSkillsAcrossRestoreAndRetry(t *testing.T) {
	for _, text := range []string{"$specflow changed", "changed without skill"} {
		t.Run(text, func(t *testing.T) {
			desktop, backend := editTestApp()
			backend.skills = make(chan []agent.Skill, 2)
			backend.result.Skills = []agent.Skill{{Name: "specflow", Path: "/skills/specflow/SKILL.md"}}
			queue := make(chan func(), 10)
			desktop.dispatch = func(update func()) { queue <- update }
			desktop.beginMessageEdit(desktop.State.Messages[2])
			desktop.edit.Text = text
			if err := desktop.installEditedHistory(backend.result); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(desktop.edit)
			if err != nil {
				t.Fatal(err)
			}
			desktop.edit = &storage.MessageEdit{}
			if err := json.Unmarshal(raw, desktop.edit); err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				desktop.sendMessageEdit()
				select {
				case <-backend.sends:
				case <-time.After(3 * time.Second):
					t.Fatal("edited message was not sent")
				}
				var skills []agent.Skill
				select {
				case skills = <-backend.skills:
				default:
				}
				if strings.HasPrefix(text, "$specflow") {
					if len(skills) != 1 || skills[0].Name != "specflow" || skills[0].Path != "/skills/specflow/SKILL.md" {
						t.Fatalf("skill reference lost: %+v", skills)
					}
				} else if len(skills) != 0 {
					t.Fatalf("removed skill still sent: %+v", skills)
				}
				pumpUpdate(t, queue)
				desktop.applySubmissionFailure(desktop.State.Sequence, text, &agent.NotSubmittedError{Err: errors.New("test rejection")}, true)
			}
			select {
			case <-backend.edits:
				t.Fatal("prepared retry forked history again")
			default:
			}
		})
	}
}

func (backend *editTestBackend) ResumeThread(_ context.Context, id string) (agent.ResumedThread, error) {
	return agent.ResumedThread{ThreadMeta: agent.ThreadMeta{ID: id, Status: agent.ThreadStatus{Type: "idle"}}, Cwd: "/project"}, nil
}

func (backend *editTestBackend) ListThreads(context.Context, *string) (agent.ThreadsPage, error) {
	return agent.ThreadsPage{}, nil
}

func (backend *editTestBackend) ListTurns(_ context.Context, _ string, _ *string) (agent.TurnsPage, error) {
	turns := slices.Clone(backend.result.Turns)
	slices.Reverse(turns)
	return agent.TurnsPage{Data: turns}, nil
}

func (backend *editTestBackend) EditHistory(_ context.Context, thread, turn, message string) (agent.HistoryEdit, error) {
	backend.edits <- thread + "/" + turn + "/" + message
	return backend.result, backend.err
}

func (backend *editTestBackend) Send(_ context.Context, thread, text string) (agent.Turn, error) {
	backend.sends <- thread + "/" + text
	return agent.Turn{ID: "edited-turn"}, nil
}

func (backend *editTestBackend) Interrupt(_ context.Context, thread, turn string) error {
	backend.stops <- thread + "/" + turn
	return nil
}

func editTestApp() (*App, *editTestBackend) {
	backend := &editTestBackend{edits: make(chan string, 4), sends: make(chan string, 4), stops: make(chan string, 4)}
	desktop := New(backend, "test", "/project", nil)
	desktop.State.ThreadID = "original"
	for _, number := range []string{"1", "2", "3"} {
		desktop.State.Messages = append(desktop.State.Messages,
			&app.Message{ID: "user-" + number, NativeID: "native-" + number, TurnID: "turn-" + number, Role: "user", Text: "question " + number, Done: true},
			&app.Message{ID: "answer-" + number, TurnID: "turn-" + number, Role: "assistant", Text: "answer " + number, Done: true})
	}
	desktop.State.Sequence = 3
	backend.result = agent.HistoryEdit{
		Thread: agent.ResumedThread{ThreadMeta: agent.ThreadMeta{ID: "replacement", Status: agent.ThreadStatus{Type: "idle"}}, Cwd: "/project"},
		Turns:  []agent.HistoryTurn{{ID: "turn-1", Status: "completed", ItemsView: "full", Items: []json.RawMessage{json.RawMessage(`{"id":"native-1","type":"userMessage","content":[{"type":"text","text":"question 1"}]}`), json.RawMessage(`{"id":"answer-1","type":"agentMessage","text":"answer 1"}`)}}},
	}
	return desktop, backend
}

func TestMessageEditUIAndDraftIsolation(t *testing.T) {
	for _, dark := range []bool{false, true} {
		desktop, _ := editTestApp()
		message := desktop.State.Messages[2]
		message.SentAt = time.Date(2026, 11, 3, 11, 11, 0, 0, time.Local)
		desktop.State.Draft = "main draft"
		tester := native.NewTester(func(c *native.Context) { desktop.message(c, message) }, 700, 500)
		tester.SetDark(dark)
		before, _ := tester.Find("question 2")
		tester.Move(before.X+4, before.Y+4)
		if !tester.HasText("11:11") {
			t.Fatal("missing send time")
		}
		if err := tester.Click("复制消息"); err != nil || tester.Clipboard() != "question 2" {
			t.Fatal("copy failed", err)
		}
		after, _ := tester.Find("question 2")
		if before != after {
			t.Fatal("hover moved bubble")
		}
		if err := tester.Click("编辑消息"); err != nil {
			t.Fatal(err)
		}
		if !tester.Focused("编辑消息内容") || !desktop.canSendMessageEdit() {
			t.Fatal("editor not focused or unchanged edit not sendable")
		}
		tester.Type(" changed")
		tester.Key(native.Cmd, native.KeyEnter)
		if !desktop.canSendMessageEdit() || desktop.State.Draft != "main draft" || !strings.Contains(desktop.edit.Text, "\n") {
			t.Fatal("editing damaged draft")
		}
		tester.Key(native.Cmd, native.KeyA)
		tester.Type("   ")
		if desktop.canSendMessageEdit() {
			t.Fatal("blank edit sendable")
		}
		tester.Key(0, native.KeyEscape)
		if desktop.edit != nil || message.Text != "question 2" || desktop.State.Draft != "main draft" {
			t.Fatal("cancel changed message or draft")
		}
	}
}

func TestMessageEditEnterSendsFromEditor(t *testing.T) {
	for _, suffix := range []string{"", " changed"} {
		t.Run("suffix="+suffix, func(t *testing.T) {
			desktop, backend := editTestApp()
			queue := make(chan func(), 10)
			desktop.dispatch = func(update func()) { queue <- update }
			message := desktop.State.Messages[2]
			desktop.beginMessageEdit(message)
			tester := native.NewTester(func(c *native.Context) { desktop.message(c, message) }, 700, 400)
			if suffix != "" {
				tester.Type(suffix)
			}
			tester.Key(0, native.KeyEnter)
			pumpUpdate(t, queue)
			pumpUpdate(t, queue)
			if boundary := <-backend.edits; boundary != "original/turn-2/native-2" {
				t.Fatal(boundary)
			}
			if sent := <-backend.sends; sent != "replacement/question 2"+suffix {
				t.Fatal(sent)
			}
		})
	}
}

func TestMessageEditCollapsesToBubbleWhileSubmitting(t *testing.T) {
	desktop, backend := editTestApp()
	queue := make(chan func(), 10)
	desktop.dispatch = func(update func()) { queue <- update }
	message := desktop.State.Messages[2]
	desktop.beginMessageEdit(message)
	desktop.edit.Text = "  edited question  "
	tester := native.NewTester(func(c *native.Context) { desktop.message(c, message) }, 700, 400)
	desktop.sendMessageEdit()
	tester.Frame()
	bubble, found := tester.Find("edited question")
	if !found {
		t.Fatal("edited text not rendered as a bubble during submit")
	}
	if _, found := tester.Find("编辑消息内容"); found {
		t.Fatal("edit box still visible during submit")
	}
	tester.Move(bubble.X+4, bubble.Y+4)
	if err := tester.Click("复制消息"); err != nil || tester.Clipboard() != "edited question" {
		t.Fatal("copy did not use the shown text", err)
	}
	pumpUpdate(t, queue)
	pumpUpdate(t, queue)
	if sent := <-backend.sends; sent != "replacement/edited question" {
		t.Fatal(sent)
	}
}

func TestMessageEditRestoresEditorOnPrepareFailure(t *testing.T) {
	desktop, backend := editTestApp()
	queue := make(chan func(), 10)
	desktop.dispatch = func(update func()) { queue <- update }
	backend.err = errors.New("unsupported boundary")
	message := desktop.State.Messages[2]
	desktop.beginMessageEdit(message)
	desktop.edit.Text = "edited question"
	tester := native.NewTester(func(c *native.Context) { desktop.message(c, message) }, 700, 400)
	desktop.sendMessageEdit()
	pumpUpdate(t, queue)
	tester.Frame()
	if _, found := tester.Find("编辑消息内容"); !found {
		t.Fatal("edit box not restored after failed preparation")
	}
	if !tester.HasText("编辑未发送") {
		t.Fatal("error not surfaced in the restored edit box")
	}
}

func TestComposerEscapeDismissesCompletionBeforeStopping(t *testing.T) {
	desktop := completionTestApp()
	backend := &editTestBackend{stops: make(chan string, 2)}
	desktop.backend = backend
	desktop.State.ThreadID, desktop.State.TurnID, desktop.State.Busy = "thread", "turn", true
	tester := native.NewTester(func(c *native.Context) { desktop.composer(c) }, 700, 400)
	if err := tester.Click("任务输入"); err != nil {
		t.Fatal(err)
	}
	tester.Type("$front")
	if _, found := tester.Find("输入候选"); !found {
		t.Fatal("completion not open")
	}
	tester.Key(0, native.KeyEscape)
	if desktop.State.Stopping() {
		t.Fatal("Escape stopped before dismissing completion")
	}
	tester.Key(0, native.KeyEscape)
	select {
	case <-backend.stops:
	case <-time.After(time.Second):
		t.Fatal("second Escape did not stop")
	}
}

func TestMessageEditReplacesPrefixAndKeepsOwner(t *testing.T) {
	desktop, backend := editTestApp()
	queue := make(chan func(), 20)
	desktop.dispatch = func(update func()) { queue <- update }
	desktop.State.Draft = "main draft"
	owner := desktop.session
	desktop.beginMessageEdit(desktop.State.Messages[2])
	desktop.edit.Text = "edited question"
	desktop.sendMessageEdit()
	desktop.sendMessageEdit()
	desktop.newSession("/other")
	desktop.State.Draft = "other draft"
	pumpUpdate(t, queue)
	pumpUpdate(t, queue)
	if sent := <-backend.sends; sent != "replacement/edited question" {
		t.Fatal(sent)
	}
	if len(backend.edits) != 1 || owner.State.ThreadID != "replacement" || owner.State.Draft != "main draft" || desktop.State.Draft != "other draft" || len(owner.State.Messages) != 3 {
		t.Fatalf("incorrect owner or history: %+v", owner.State)
	}
	if !desktop.threadDeleted(agent.Codex, "", "original") || !slices.Contains(owner.saved.ReplacedThreads, "original") {
		t.Fatal("original thread can reappear")
	}
	bound := desktop.forSession(owner)
	bound.State.BindTiming("edited-turn")
	bound.applySubmissionFailure(bound.State.Sequence, "edited question", &agent.NotSubmittedError{Err: errors.New("test rejection")}, true)
	if owner.State.Draft != "main draft" || owner.edit == nil || owner.editSending || !owner.edit.Prepared {
		t.Fatal("failed edit damaged main draft or lost retry")
	}
	bound.sendMessageEdit()
	pumpUpdate(t, queue)
	if len(backend.edits) != 1 || <-backend.sends != "replacement/edited question" || len(owner.State.Messages) != 3 {
		t.Fatal("retry truncated history again or duplicated prompt")
	}
}

func TestMessageEditFailureKeepsHistoryAndRestartDraft(t *testing.T) {
	desktop, backend := editTestApp()
	queue := make(chan func(), 10)
	desktop.dispatch = func(update func()) { queue <- update }
	backend.err = errors.New("unsupported boundary")
	desktop.beginMessageEdit(desktop.State.Messages[2])
	desktop.edit.Text = "modified"
	desktop.sendMessageEdit()
	pumpUpdate(t, queue)
	if len(desktop.State.Messages) != 6 || desktop.State.ThreadID != "original" || desktop.editError == "" || desktop.edit.Text != "modified" || len(backend.sends) != 0 {
		t.Fatal("failed preparation changed history")
	}
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	desktop.store = store
	if err := desktop.persist(); err != nil {
		t.Fatal(err)
	}
	reopened := New(backend, "test", "/project", nil)
	if err := reopened.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if reopened.edit == nil || reopened.edit.Text != "modified" {
		t.Fatal("restart lost edit draft")
	}
}

func TestPreparedEditResetsProcessGenerationAndKeepsAttachments(t *testing.T) {
	desktop, backend := editTestApp()
	queue := make(chan func(), 10)
	desktop.dispatch = func(update func()) { queue <- update }
	image, _ := imageFixture(t)
	message := desktop.State.Messages[2]
	message.Images = []attachment.Image{image}
	desktop.State.DraftImages = []attachment.Image{image}
	desktop.generation = 9
	desktop.beginMessageEdit(message)
	if desktop.edit == nil || len(desktop.edit.Images) != 1 {
		t.Fatal("image lost on edit")
	}
	desktop.edit.Images[0].Name = "edited reference"
	if message.Images[0].Name == "edited reference" || desktop.State.DraftImages[0].Name == "edited reference" {
		t.Fatal("attachments aliased")
	}
	desktop.edit.Text = "changed"
	if err := desktop.installEditedHistory(backend.result); err != nil {
		t.Fatal(err)
	}
	if desktop.generation != 0 || desktop.State.DraftImages[0] != image {
		t.Fatal("new thread inherited generation or changed draft")
	}
	desktop.cancelMessageEdit()
	pumpUpdate(t, queue)
	if len(desktop.State.Messages) != 2 || desktop.edit != nil {
		t.Fatal("cancel retained a synthetic history message")
	}
}

func TestRestartConfirmsEditedSubmissionBeforeResending(t *testing.T) {
	desktop, backend := editTestApp()
	desktop.beginMessageEdit(desktop.State.Messages[2])
	desktop.edit.Text = "changed"
	if err := desktop.installEditedHistory(backend.result); err != nil {
		t.Fatal(err)
	}
	desktop.edit.Submitted = true
	desktop.removeEditPlaceholder()
	desktop.State.Messages = append(desktop.State.Messages, &app.Message{ID: "native-edited", NativeID: "native-edited", Role: "user", Text: "changed", TurnID: "new-turn", Done: true})
	desktop.restoreMessageEdit()
	if desktop.edit != nil || len(desktop.State.Messages) != 3 {
		t.Fatal("confirmed edit was duplicated on restart")
	}
}

func TestComposerEscapeStopsOnceAndKeepsDraft(t *testing.T) {
	desktop, backend := editTestApp()
	desktop.State.Busy, desktop.State.TurnID, desktop.State.Draft = true, "running", "draft"
	tester := native.NewTester(func(c *native.Context) { desktop.composer(c) }, 700, 200)
	if err := tester.Click("任务输入"); err != nil {
		t.Fatal(err)
	}
	tester.Key(0, native.KeyEscape)
	tester.Key(0, native.KeyEscape)
	select {
	case stopped := <-backend.stops:
		if stopped != "original/running" {
			t.Fatal(stopped)
		}
	case <-time.After(time.Second):
		t.Fatal("Escape did not stop")
	}
	if desktop.State.Draft != "draft" || !desktop.State.Stopping() {
		t.Fatal("Escape damaged draft")
	}
}

func TestMessageEditScreenshots(t *testing.T) {
	directory := os.Getenv("SASA_EDIT_SNAPSHOTS")
	if directory == "" {
		t.Skip("set SASA_EDIT_SNAPSHOTS to save component renders")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		desktop, _ := editTestApp()
		message := desktop.State.Messages[2]
		message.Text = "这个 ui 交互优化一下"
		message.SentAt = time.Date(2026, 11, 3, 11, 11, 0, 0, time.Local)
		tester := native.NewTester(func(c *native.Context) {
			native.Column(c).Fill().Padding(24).Children(func() { desktop.message(c, message) })
		}, 800, 300)
		tester.SetDark(dark)
		theme := "light"
		if dark {
			theme = "dark"
		}
		bubble, _ := tester.Find(message.Text)
		tester.Move(bubble.X+10, bubble.Y+10)
		for _, editing := range []bool{false, true} {
			state := "hover"
			if editing {
				desktop.beginMessageEdit(message)
				tester.Frame()
				state = "edit"
			}
			file, err := os.Create(filepath.Join(directory, theme+"-"+state+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(file, tester.Image())
			file.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestMessageEditKeepsReadingPosition(t *testing.T) {
	desktop, _ := editTestApp()
	for _, message := range desktop.State.Messages {
		if message.Role == "assistant" {
			message.Text = strings.Repeat("A paragraph for scrolling.\n\n", 30)
		}
	}
	anchorID := ""
	var anchor native.Rect
	tester := native.NewTester(func(c *native.Context) {
		desktop.view(c)
		if anchorID != "" {
			anchor = desktop.rowHandle(anchorID).Bounds(c)
		}
	}, 1000, 760)
	tester.Scroll(600, 300, 0, -600)
	first, _ := desktop.list.Visible()
	anchorID = desktop.displayMessages()[first].ID
	tester.Frame()
	before := anchor.Y
	message := desktop.State.Messages[2]
	desktop.beginMessageEdit(message)
	for frame := 0; frame < 3; frame++ {
		tester.Frame()
	}
	if anchor.Y != before || desktop.list.FollowEnd {
		t.Fatalf("editing moved reading anchor: %f -> %f", before, anchor.Y)
	}
	desktop.cancelMessageEdit()
	for frame := 0; frame < 3; frame++ {
		tester.Frame()
	}
	if anchor.Y != before {
		t.Fatalf("cancel moved reading anchor: %f -> %f", before, anchor.Y)
	}
}
