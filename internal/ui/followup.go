package ui

import (
	"crypto/rand"
	"errors"
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

func (a *App) enqueueInput(text string, images []attachment.Image) {
	for _, image := range images {
		if _, err := attachment.Read(image); err != nil {
			a.State.Error = err.Error()
			return
		}
	}
	var identity [16]byte
	if _, err := rand.Read(identity[:]); err != nil {
		a.State.Error = err.Error()
		return
	}
	identity[6], identity[8] = identity[6]&0x0f|0x40, identity[8]&0x3f|0x80
	effort, tier := a.sendTurnOptions()
	pending := storage.PendingInput{
		CreatedAt: time.Now(),
		ID:        fmt.Sprintf("%x-%x-%x-%x-%x", identity[:4], identity[4:6], identity[6:8], identity[8:10], identity[10:]),
		Text:      text, Images: images, Skills: a.draftSkills(text), Status: "queued",
		Options: agent.TurnOptions{Model: a.selectedModel, Effort: effort, ServiceTier: tier, Mode: a.selectedMode},
	}
	a.pendingInputs = append(a.pendingInputs, pending)
	if _, ok := a.sessionBackend().(agent.TurnSteerer); ok && a.State.Busy {
		a.pendingInputs[len(a.pendingInputs)-1].TurnID = a.State.TurnID
	}
	a.restorePendingMessages()
	draft, draftImages := a.State.Draft, a.State.DraftImages
	a.State.Draft, a.State.DraftImages = "", nil
	if err := a.persist(); err != nil {
		a.pendingInputs = a.pendingInputs[:len(a.pendingInputs)-1]
		a.removeInputMessage(pending.ID)
		a.State.Draft, a.State.DraftImages = draft, draftImages
		a.State.Error = "消息未能保存，本次未发送：" + err.Error()
		return
	}
	a.list.FollowEnd = true
	a.list.ScrollToEnd()
	a.drainInputs()
}

func (a *App) restorePendingMessages() {
	for _, pending := range a.pendingInputs {
		if slices.ContainsFunc(a.State.Messages, func(message *app.Message) bool { return message.ID == pending.ID }) {
			continue
		}
		a.State.Messages = append(a.State.Messages, &app.Message{
			ID: pending.ID, Role: "user", Text: pending.Text, Images: slices.Clone(pending.Images),
			TurnID: pending.TurnID, Delivery: "queued", Done: true, SentAt: pending.CreatedAt,
		})
	}
}

func (a *App) removeInputMessage(id string) {
	a.State.Messages = slices.DeleteFunc(a.State.Messages, func(message *app.Message) bool { return message.ID == id && message.NativeID == "" })
}

func (a *App) pauseInputs() {
	for index := range a.pendingInputs {
		if a.pendingInputs[index].Status == "queued" {
			a.pendingInputs[index].Status = "paused"
		}
	}
}

func (a *App) settleInputs() {
	if len(a.pendingInputs) == 0 {
		return
	}
	for _, message := range slices.Clone(a.State.Messages) {
		if message.Role != "user" || message.NativeID == "" {
			continue
		}
		for index, pending := range a.pendingInputs {
			if pending.Status != "sending" && pending.Status != "accepted" && pending.Status != "uncertain" || pending.TurnID != message.TurnID || slices.Contains(pending.KnownIDs, message.NativeID) || pending.Text != message.Text || len(pending.Images) != len(message.Images) {
				continue
			}
			matched := true
			for imageIndex, image := range pending.Images {
				if image.Path != message.Images[imageIndex].Path || image.URL != message.Images[imageIndex].URL {
					matched = false
					break
				}
			}
			if matched {
				for _, local := range a.State.Messages {
					if local.ID == pending.ID && local != message {
						local.NativeID, local.TurnID, local.Delivery = message.NativeID, message.TurnID, message.Delivery
						a.State.Messages = slices.DeleteFunc(a.State.Messages, func(candidate *app.Message) bool { return candidate == message })
						break
					}
				}
				a.pendingInputs = slices.Delete(a.pendingInputs, index, index+1)
				for remaining := range a.pendingInputs {
					if a.pendingInputs[remaining].TurnID == message.TurnID {
						a.pendingInputs[remaining].KnownIDs = append(a.pendingInputs[remaining].KnownIDs, message.NativeID)
					}
				}
				break
			}
		}
	}
}

func (a *App) reconcileInputDelivery() {
	if a.State.Busy || a.State.Disconnected || a.State.Incomplete {
		return
	}
	for _, pending := range a.pendingInputs {
		if pending.TurnID == a.State.TurnID && (pending.Status == "sending" || pending.Status == "accepted" || pending.Status == "uncertain") {
			a.State.NeedsSnapshot = true
			return
		}
	}
}

func (a *App) drainInputs() {
	if a.followupSending || !a.canComposeTask() {
		return
	}
	index := 0
	for index < len(a.pendingInputs) && a.pendingInputs[index].Status == "accepted" {
		index++
	}
	if index >= len(a.pendingInputs) || a.pendingInputs[index].Status != "queued" {
		return
	}
	pending := a.pendingInputs[index]
	if !a.State.Busy {
		if a.snapshotPending || a.State.NeedsSnapshot || a.State.Incomplete {
			return
		}
		draft, images := a.State.Draft, a.State.DraftImages
		model, effort, tier := a.selectedModel, a.selectedEffort, a.selectedTier
		mode := a.selectedMode
		a.selectedMode = pending.Options.Mode
		a.selectedModel, a.selectedEffort, a.selectedTier = pending.Options.Model, pending.Options.Effort, pending.Options.ServiceTier
		sequence := a.State.Sequence
		for _, skill := range pending.Skills {
			if a.completion.chosen == nil {
				a.completion.chosen = make(map[string]agent.Skill)
			}
			a.completion.chosen[skill.Name] = skill
		}
		a.pendingInputs = slices.Delete(a.pendingInputs, index, index+1)
		a.sendInputForMessage(pending.Text, pending.Images, false, pending.ID)
		a.selectedModel, a.selectedEffort, a.selectedTier = model, effort, tier
		a.selectedMode = mode
		a.State.Draft, a.State.DraftImages = draft, images
		if a.State.Sequence == sequence || !a.State.Busy {
			pending.Status, pending.Error = "failed", a.State.Error
			a.pendingInputs = append([]storage.PendingInput{pending}, a.pendingInputs...)
		}
		return
	}
	if isCompactCommand(pending.Text) || a.State.Compacting() {
		return
	}
	sender, ok := a.sessionBackend().(agent.TurnSteerer)
	if !ok || pending.Options.Mode != a.activeMode || a.State.TurnID == "" || a.submitCancel != nil || a.State.PendingSubmission() != nil {
		return
	}
	threadID, turnID := a.State.ThreadID, a.State.TurnID
	a.pendingInputs[index].Status, a.pendingInputs[index].TurnID = "sending", turnID
	for _, message := range a.State.Messages {
		if message.ID == pending.ID {
			message.TurnID = turnID
		}
		if message.Role == "user" && message.NativeID != "" {
			a.pendingInputs[index].KnownIDs = append(a.pendingInputs[index].KnownIDs, message.NativeID)
		}
	}
	if err := a.persist(); err != nil {
		a.pendingInputs[index].Status, a.pendingInputs[index].Error = "failed", "消息未能保存，本次未发送："+err.Error()
		return
	}
	a.followupSending = true
	go func() {
		ctx, cancel := agent.RequestContext()
		defer cancel()
		err := sender.Steer(ctx, threadID, turnID, pending.ID, pending.Text, pending.Skills, pending.Images)
		a.update(func() {
			a.followupSending = false
			if a.State.ThreadID != threadID {
				return
			}
			index := slices.IndexFunc(a.pendingInputs, func(input storage.PendingInput) bool { return input.ID == pending.ID })
			if err == nil {
				if index >= 0 {
					a.pendingInputs[index].Status = "accepted"
				}
				a.list.ScrollToEnd()
			} else if index >= 0 {
				status := "uncertain"
				var rejected *agent.RPCError
				if errors.As(err, &rejected) {
					status = "failed"
				}
				a.pendingInputs[index].Status, a.pendingInputs[index].Error = status, err.Error()
			}
			a.settleInputs()
			a.reconcileInputDelivery()
			a.reconcileIfNeeded()
			a.drainInputs()
		})
	}()
}

func (a *App) retrieveInput(id string) {
	if a.followupSending || strings.TrimSpace(a.State.Draft) != "" || len(a.State.DraftImages) != 0 {
		return
	}
	index := slices.IndexFunc(a.pendingInputs, func(input storage.PendingInput) bool { return input.ID == id })
	if index < 0 || a.pendingInputs[index].Status == "sending" {
		return
	}
	pending := a.pendingInputs[index]
	a.State.Draft, a.State.DraftImages = pending.Text, slices.Clone(pending.Images)
	for _, skill := range pending.Skills {
		if a.completion.chosen == nil {
			a.completion.chosen = make(map[string]agent.Skill)
		}
		a.completion.chosen[skill.Name] = skill
	}
	a.pendingInputs = slices.Delete(a.pendingInputs, index, index+1)
	a.removeInputMessage(id)
	a.composerFocus = true
}

func (a *App) inputFeedback(c *native.Context, id string) {
	index := slices.IndexFunc(a.pendingInputs, func(input storage.PendingInput) bool { return input.ID == id })
	if index < 0 {
		return
	}
	pending := a.pendingInputs[index]
	label := ""
	switch pending.Status {
	case "paused":
		label = "已暂停发送"
	case "failed":
		label = "未发送"
	case "uncertain":
		label = "发送结果待核验，请先检查会话记录"
	default:
		return
	}
	native.Row(c.Key("input-feedback:" + id)).FillWidth().Gap(8).Justify(native.End).AlignItems(native.Center).Children(func() {
		native.Text(c, label).FontSize(11).TextColor(c.Theme().TextMuted).Tooltip(pending.Error)
		if pending.Status == "paused" || pending.Status == "failed" {
			if actionButton(c, "继续发送").Disabled(!a.canComposeTask() || a.followupSending).Clicked() {
				a.pendingInputs[index].Status, a.pendingInputs[index].Error = "queued", ""
				a.drainInputs()
			}
		}
		if actionButton(c, "取回编辑").Disabled(a.followupSending || a.State.Draft != "" || len(a.State.DraftImages) > 0).Clicked() {
			a.retrieveInput(id)
		}
		remove := quietIconButton(c, "移除消息").Size(24, 24).Disabled(a.followupSending)
		remove.Children(func() { native.Icon(c, removeImageIcon).Size(14, 14) })
		if remove.Clicked() {
			a.pendingInputs = slices.DeleteFunc(a.pendingInputs, func(input storage.PendingInput) bool { return input.ID == id })
			a.removeInputMessage(id)
			a.drainInputs()
		}
	})
}
