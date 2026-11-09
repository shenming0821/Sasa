package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/attachment"
	"github.com/shenming0821/Fuyu/internal/storage"
)

type messageEditing struct {
	edit            *storage.MessageEdit
	editPreparing   bool
	editSending     bool
	editFocus       bool
	editError       string
	editScrollHold  int
	userCopiedID    string
	userCopiedUntil time.Time
}

var messageEditIcon = icon(`<path d="m14 5 5 5M4 20l5-1L21 7a2.1 2.1 0 0 0-4-4L5 15l-1 5Z"/>`)

func (a *App) messageEditReason(message *app.Message) string {
	if _, ok := a.sessionBackend().(agent.HistoryEditor); !ok {
		return "当前 Agent 暂不支持编辑历史消息"
	}
	if a.edit != nil && a.edit.ID != message.ID {
		return "请先完成当前消息编辑"
	}
	if a.State.Busy || a.editPreparing || a.editSending {
		return "任务结束后可编辑"
	}
	if !a.canSubmitTask() || a.historyLoading || a.snapshotPending || a.State.Incomplete || a.State.NeedsSnapshot || a.forkUncertain || a.reviewRequired || a.State.PendingSubmission() != nil {
		return "会话核验完成后可编辑"
	}
	if message.NativeID == "" || message.TurnID == "" || message.Delivery == "unsent" {
		return "此消息尚无已确认的原生记录"
	}
	if strings.HasPrefix(strings.TrimSpace(message.Text), "/") {
		return "命令消息暂不支持历史编辑"
	}
	return ""
}

func (a *App) beginMessageEdit(message *app.Message) {
	if a.messageEditReason(message) != "" {
		return
	}
	a.edit = &storage.MessageEdit{ID: message.ID, NativeID: message.NativeID, TurnID: message.TurnID, Text: message.Text, Original: message.Text, Images: slices.Clone(message.Images)}
	a.editFocus, a.editError = true, ""
	a.editScrollHold = 2
	a.list.FollowEnd = false
}

func (a *App) cancelMessageEdit() {
	if a.editPreparing || a.editSending {
		return
	}
	if a.edit != nil && a.edit.Prepared {
		a.cancelPreparedMessageEdit()
		return
	}
	a.edit, a.editError, a.editFocus = nil, "", false
	a.editScrollHold = 2
	a.list.FollowEnd = false
}

func (a *App) cancelPreparedMessageEdit() {
	if a.edit.Submitted || a.State.Disconnected || a.State.Incomplete || a.State.NeedsSnapshot || a.snapshotPending || a.State.Busy {
		a.editError = "请先核验本次发送结果，再取消编辑"
		return
	}
	backend, ok := a.sessionBackend().(agent.HistoryBackend)
	if !ok || len(a.saved.ReplacedThreads) == 0 {
		a.editError = "无法读取原消息，请保留编辑内容并重新核验会话"
		return
	}
	a = a.forSession(a.session)
	edit, threadID := a.edit, a.saved.ReplacedThreads[len(a.saved.ReplacedThreads)-1]
	a.editPreparing = true
	go func() {
		ctx, cancel := agent.RequestContext()
		defer cancel()
		thread, err := backend.ResumeThread(ctx, threadID)
		var turns []agent.HistoryTurn
		if err == nil {
			turns, err = agent.ReadHistory(ctx, backend, threadID)
		}
		if err == nil {
			page := agent.TurnsPage{Data: turns}
			a.materializeHistoryImages(&page)
			turns = page.Data
		}
		a.update(func() {
			a.editPreparing = false
			if a.edit != edit {
				return
			}
			if err == nil {
				err = a.switchEditedHistory(agent.HistoryEdit{Thread: thread, Turns: turns}, true)
			}
			if err != nil {
				a.editError = "取消编辑失败，修改内容已保留：" + err.Error()
			}
		})
	}()
}

func (a *App) canSendMessageEdit() bool {
	return a.edit != nil && !a.editSending && !a.historyLoading && !a.snapshotPending && !a.State.Incomplete && !a.State.NeedsSnapshot && a.canSubmitTask() &&
		strings.TrimSpace(a.edit.Text) != ""
}

func (a *App) sendMessageEdit() {
	if !a.canSendMessageEdit() {
		return
	}
	a = a.forSession(a.session)
	edit := a.edit
	if len(edit.Images) > 0 {
		if _, ok := a.sessionBackend().(imageSender); !ok {
			a.editError = "当前连接不支持图片输入"
			return
		}
	}
	if a.selectedModel != "" {
		if _, ok := a.sessionBackend().(modelSender); !ok {
			a.editError = "当前连接不支持切换模型"
			return
		}
	}
	for _, image := range edit.Images {
		if _, err := attachment.Read(image); err != nil {
			a.editError = err.Error()
			return
		}
	}
	if edit.Prepared {
		a.sendInput(strings.TrimSpace(edit.Text), slices.Clone(edit.Images), true)
		return
	}
	backend, ok := a.sessionBackend().(agent.HistoryEditor)
	if !ok {
		return
	}
	a.editPreparing, a.editError = true, ""
	threadID := a.State.ThreadID
	go func() {
		ctx, cancel := agent.RequestContext()
		defer cancel()
		result, err := backend.EditHistory(ctx, threadID, edit.TurnID, edit.NativeID)
		if err == nil {
			page := agent.TurnsPage{Data: result.Turns}
			a.materializeHistoryImages(&page)
			result.Turns = page.Data
		}
		a.update(func() {
			a.editPreparing = false
			if a.edit != edit || a.State.ThreadID != threadID {
				return
			}
			if err != nil {
				a.editError = "编辑未发送：" + err.Error()
				return
			}
			if err := a.installEditedHistory(result); err != nil {
				a.editError = "编辑未发送：" + err.Error()
				return
			}
			if a.open {
				a.sendInput(strings.TrimSpace(edit.Text), slices.Clone(edit.Images), true)
			}
		})
	}()
}

func (a *App) installEditedHistory(result agent.HistoryEdit) error {
	return a.switchEditedHistory(result, false)
}

func (a *App) switchEditedHistory(result agent.HistoryEdit, restoring bool) error {
	thread := result.Thread
	if thread.ThreadMeta.ID == "" || thread.ThreadMeta.ID == a.State.ThreadID || thread.Cwd != a.State.Cwd {
		return fmt.Errorf("新的会话标识或目录不匹配")
	}
	state, unfinished, err := app.History(thread.Cwd, thread.ThreadMeta.ID, result.Turns)
	if err != nil {
		return err
	}
	if unfinished || thread.ThreadMeta.Status.Type != "idle" {
		return fmt.Errorf("新的会话仍有未核验任务")
	}
	previous, previousSaved := a.State, a.saved
	previousEdit, editValue := a.edit, *a.edit
	state.Agent = a.kind.Normalized()
	state.Model, state.Policy, state.Sandbox = thread.Model, string(thread.Policy), string(thread.Sandbox)
	state.RuntimeVersion = thread.RuntimeVersion
	state.Draft, state.DraftImages = previous.Draft, previous.DraftImages
	state.Sequence = max(state.Sequence, previous.Sequence)
	timings := make(map[string]app.TurnTiming)
	for _, message := range previous.Messages {
		if message.Timing != nil && message.TurnID != "" {
			timings[message.TurnID] = *message.Timing
		}
	}
	state.RestoreTimings(timings)
	a.saved.ReplacedThreads = append(slices.Clone(a.saved.ReplacedThreads), previous.ThreadID)
	if restoring {
		a.saved.ReplacedThreads = slices.DeleteFunc(a.saved.ReplacedThreads, func(id string) bool { return id == state.ThreadID })
	}
	a.saved.TurnTimings = nil
	a.saved.Expanded = nil
	a.saved.Anchor, a.saved.TurnID, a.saved.Active = "", "", false
	if restoring {
		a.edit = nil
		a.editError, a.editFocus = "", false
	} else {
		a.edit.Prepared = true
		a.edit.PrefixTurnID = state.TurnID
	}
	a.State = state
	a.restoreMessageEdit()
	if err := a.persist(); err != nil {
		a.State, a.saved = previous, previousSaved
		a.edit = previousEdit
		*a.edit = editValue
		return err
	}
	a.historyCursor = nil
	a.generation = 0
	a.historyAtTop, a.historyOverflow, a.reviewRequired = false, false, false
	a.recoveryEpoch++
	a.recoveryEvents, a.recoveryBytes = nil, 0
	a.conversationCache = conversationCache{}
	a.conversationNavigation = conversationNavigation{}
	a.editScrollHold = 2
	a.list.FollowEnd = false
	return nil
}

func (a *App) restoreMessageEdit() {
	if a.edit == nil {
		return
	}
	if a.edit.Prepared && a.edit.Submitted {
		for index := len(a.State.Messages) - 1; index >= 0; index-- {
			message := a.State.Messages[index]
			if message.Role != "user" {
				continue
			}
			if message.TurnID != "" && message.TurnID != a.edit.PrefixTurnID && message.Text == strings.TrimSpace(a.edit.Text) && len(message.Images) == len(a.edit.Images) {
				a.edit, a.editSending = nil, false
				return
			}
			break
		}
	}
	for _, message := range a.State.Messages {
		if a.edit.SubmittedTurnID != "" && message.Role == "user" && message.TurnID == a.edit.SubmittedTurnID {
			a.edit, a.editSending = nil, false
			return
		}
		if message.ID == a.edit.ID || message.NativeID != "" && message.NativeID == a.edit.NativeID && !a.edit.Prepared {
			a.edit.ID = message.ID
			return
		}
	}
	if a.edit.Prepared {
		a.State.Messages = append(a.State.Messages, &app.Message{ID: a.edit.ID, Role: "user", Text: a.edit.Text, Images: slices.Clone(a.edit.Images), Done: true, Edited: true})
	}
}

func (a *App) removeEditPlaceholder() {
	if a.edit == nil {
		return
	}
	a.State.Messages = slices.DeleteFunc(a.State.Messages, func(message *app.Message) bool { return message.ID == a.edit.ID })
}

func (a *App) lastMessageEdited() bool {
	for index := len(a.State.Messages) - 1; index >= 0; index-- {
		if message := a.State.Messages[index]; message.Role == "user" {
			return message.Edited
		}
	}
	return false
}

func (a *App) editSendFailed() {
	if a.edit != nil && a.editSending {
		a.editSending = false
		a.edit.Submitted = a.State.Disconnected
		a.editError = a.State.Error
		a.editFocus = true
	}
}

func (a *App) finishMessageEdit() {
	if a.edit == nil || !a.editSending {
		return
	}
	for _, message := range a.State.Messages {
		if message.ID == a.edit.ID && message.Delivery == "sent" || a.edit.SubmittedTurnID != "" && message.Role == "user" && message.TurnID == a.edit.SubmittedTurnID && message.NativeID != "" {
			a.edit, a.editSending = nil, false
			return
		}
	}
	if !a.State.Busy {
		a.editSendFailed()
	}
}

func (a *App) userMessage(c *native.Context, message *app.Message) {
	editing := a.edit != nil && a.edit.ID == message.ID
	if editing && !a.editPreparing && !a.editSending {
		a.messageEditBox(c, message)
		return
	}
	text := message.Text
	if editing {
		text = strings.TrimSpace(a.edit.Text)
	}
	theme := c.Theme()
	box := native.Column(c.Key("user-message")).FillWidth().Gap(4)
	shown := box.Hovered() || box.FocusWithin()
	box.Children(func() {
		if len(message.Images) > 0 {
			a.messageImages(c, message)
		}
		if text != "" {
			native.Row(c).FillWidth().Justify(native.End).Children(func() {
				native.Text(c, text).Selectable().FontSize(15).FontWeight(400).LineHeight(1.6).
					MaxWidthPercent(85).Padding(8, 16).Radius(20).Background(theme.Surface)
			})
		}
		bar := native.Row(c.Key("message-actions")).FillWidth().Height(28).Gap(8).Justify(native.End).AlignItems(native.Center)
		if !shown {
			bar.Opacity(0)
		}
		bar.Children(func() {
			sentAt := message.SentAt
			if sentAt.IsZero() && message.Timing != nil {
				sentAt = message.Timing.Started
			}
			if !sentAt.IsZero() {
				native.Text(c, sentAt.Local().Format("15:04")).FontSize(12).TextColor(theme.TextMuted).
					Tooltip(sentAt.Local().Format("2006-01-02 15:04:05 MST"))
			}
			glyph, label := copyIcon, "复制消息"
			if a.userCopiedID == message.ID && time.Now().Before(a.userCopiedUntil) {
				glyph, label = checkIcon, "消息已复制"
			}
			copyButton := quietIconButton(c.Key("copy"), label).Size(28, 28)
			copyButton.Children(func() { native.Icon(c, glyph).Size(16, 16) })
			if copyButton.Clicked() {
				c.WriteClipboard(text)
				a.userCopiedID, a.userCopiedUntil = message.ID, time.Now().Add(1500*time.Millisecond)
				if a.canAsync() {
					bound := a.forSession(a.session)
					until := a.userCopiedUntil
					time.AfterFunc(1500*time.Millisecond, func() {
						bound.update(func() {
							if bound.userCopiedUntil == until {
								bound.userCopiedID = ""
							}
						})
					})
				}
			}
			if _, ok := a.sessionBackend().(agent.HistoryEditor); ok {
				reason := a.messageEditReason(message)
				hint := "编辑消息"
				if reason != "" {
					hint = reason
				}
				editButton := quietIconButton(c.Key("edit"), "编辑消息").Size(28, 28).Tooltip(hint).Disabled(reason != "")
				editButton.Children(func() { native.Icon(c, messageEditIcon).Size(16, 16) })
				if editButton.Clicked() {
					a.beginMessageEdit(message)
				}
			}
		})
		if message.Delivery == "unsent" {
			native.Row(c).FillWidth().Justify(native.End).Children(func() {
				native.Text(c, "未发送").FontSize(11).TextColor(theme.TextMuted)
			})
		}
	})
}

func (a *App) messageEditBox(c *native.Context, message *app.Message) {
	edit := a.edit
	theme := c.Theme()
	native.Column(c.Key("message-edit")).FillWidth().MinWidth(0).Padding(16).Gap(14).Radius(20).Background(theme.Surface).Children(func() {
		if len(edit.Images) > 0 {
			a.messageImages(c, message)
		}
		input := native.TextAreaBase(c, &edit.Text).Label("编辑消息内容").FontSize(15).LineHeight(1.6).FillWidth().Lines(3, 12).Padding(0).
			Disabled(a.editPreparing || a.editSending)
		a.focusPressedPane(input)
		if a.editFocus && a.paneIsFocused() {
			input.Focus()
			a.editFocus = false
		}
		composing := input.Composing()
		input.OnTextKey(a.paneTextKey(func(key native.TextKey) bool {
			if composing || key.Mods != 0 {
				return false
			}
			switch key.Key {
			case native.KeyEscape:
				a.cancelMessageEdit()
				return true
			case native.KeyEnter:
				edit.Text = key.Text()
				a.sendMessageEdit()
				return true
			}
			return false
		}))
		if a.editError != "" {
			native.Text(c, a.editError).FillWidth().MinWidth(0).FontSize(12).TextColor(theme.Danger).Selectable()
		}
		native.Row(c).FillWidth().Gap(10).AlignItems(native.Center).Children(func() {
			native.Text(c, "发送后将替换此处及后续对话").Grow(1).MinWidth(0).FontSize(11).TextColor(theme.TextMuted)
			cancelButton := outlinedButton(c, "取消").Height(34).Padding(0, 12).Disabled(a.editPreparing || a.editSending)
			cancelButton.Children(func() { native.Text(c, "取消").FontSize(13) })
			if cancelButton.Clicked() {
				a.cancelMessageEdit()
			}
			if taskSendButton(c, false, !a.canSendMessageEdit(), "Enter 发送 · ⌘ Enter 换行").Clicked() {
				a.sendMessageEdit()
			}
		})
	})
}

func taskSendButton(c *native.Context, stopping, disabled bool, hint string) native.Element {
	theme := c.Theme()
	label, glyph := "发送", upIcon
	if stopping {
		label, glyph = "停止", stopIcon
	}
	button := native.ButtonBase(c).Label(label).Tooltip(hint).Size(34, 34).Radius(17).TextColor(theme.Background).Disabled(disabled)
	buttonFace(c, button, theme.Text, theme.Text.Mix(theme.Background, .12), theme.Text.Mix(theme.Background, .22), 17, 0)
	button.Children(func() { native.Icon(c, glyph).Size(20, 20) })
	return button
}

func (a *App) composerEscape() bool {
	root := a.application()
	if a.edit != nil && !a.editSending || a.detailsOpen || a.modelMenuOpen || root.settingsOpen || root.viewerOpen || root.quitDialog || root.fileAction.open || root.worktreeRenameOpen || root.sessionRenameOpen || root.worktreeCreate != nil {
		return false
	}
	if a.State.Busy {
		a.stop()
		return true
	}
	return false
}
