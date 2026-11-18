package app

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/shenming0821/Sasa/internal/attachment"
)

func (s *State) acceptUserItem(id, turnID string, raw json.RawMessage) {
	var content []struct{ Type, Text, Path, URL string }
	if len(raw) > 0 && json.Unmarshal(raw, &content) != nil {
		return
	}
	var texts []string
	var images []attachment.Image
	for _, input := range content {
		switch input.Type {
		case "text":
			texts = append(texts, input.Text)
		case "image", "localImage":
			images = append(images, attachment.Image{Path: input.Path, URL: input.URL})
		}
	}
	s.AcceptUserInput(id, turnID, bounded(strings.Join(texts, "\n")), images)
}

func (s *State) AcceptUserInput(id, turnID, text string, images []attachment.Image) {
	for _, message := range s.Messages {
		if message.Role != "user" {
			continue
		}
		if message.NativeID == id {
			if message.Text == "" && text != "" {
				message.Text = text
			}
			if len(message.Images) == 0 && len(images) > 0 {
				message.Images = slices.Clone(images)
			}
			return
		}
		if message.NativeID == "" && message.TurnID == turnID && message.Timing != nil && (text == "" || message.Text == text) {
			message.NativeID, message.Delivery = id, "sent"
			return
		}
	}
	s.Messages = append(s.Messages, &Message{
		ID: id, NativeID: id, TurnID: turnID, Role: "user", Text: text,
		Images: slices.Clone(images), Delivery: "appended", Done: true, SentAt: time.Now(),
	})
}

func (s *State) PendingSubmission() *Message {
	for index := len(s.Messages) - 1; index >= 0; index-- {
		message := s.Messages[index]
		if message.Role == "user" {
			if message.Delivery == "pending" {
				return message
			}
		}
	}
	return nil
}

func (s *State) RejectSubmission() {
	message := s.PendingSubmission()
	if message == nil {
		return
	}
	message.Delivery = "unsent"
	if message.Edited {
		s.Notice = "修改内容未发送，可在原消息处重试。"
		return
	}
	if s.Draft == "" && len(s.DraftImages) == 0 {
		s.Draft = message.Text
		s.DraftImages = slices.Clone(message.Images)
		s.Notice = "本条消息未发送，内容已恢复到输入框，请重新发送。"
	} else {
		s.Notice = "本条消息未发送，请重新编辑后发送。"
	}
}
