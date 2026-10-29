package ui

import (
	"strings"

	"github.com/shenming0821/Fuyu/internal/app"
)

func (a *App) replyCopyText(target *app.Message) string {
	if target.Role != "assistant" || !target.Done || strings.TrimSpace(target.Text) == "" {
		return ""
	}
	messages := a.State.Messages
	start, end, targetIndex := 0, len(messages), -1
	turn := ""
	for index, message := range messages {
		boundary := message.Role == "user" || message.TurnID != "" && turn != "" && message.TurnID != turn
		if boundary {
			if targetIndex >= 0 {
				end = index
				break
			}
			start, turn = index, ""
		}
		if message.TurnID != "" {
			turn = message.TurnID
		}
		if message == target {
			targetIndex = index
		}
	}
	if a.State.Busy && (target.TurnID == a.State.TurnID || end == len(messages)) {
		return ""
	}
	if targetIndex < 0 {
		return target.Text
	}
	var texts []string
	last := -1
	for index := start; index < end; index++ {
		message := messages[index]
		if message.Role != "assistant" {
			continue
		}
		if !message.Done {
			return ""
		}
		if strings.TrimSpace(message.Text) != "" {
			texts = append(texts, message.Text)
			last = index
		}
	}
	if targetIndex != last {
		return ""
	}
	return strings.Join(texts, "\n\n")
}
