package app

import "slices"

func (s *State) PendingSubmission() *Message {
	for index := len(s.Messages) - 1; index >= 0; index-- {
		message := s.Messages[index]
		if message.Role == "user" {
			if message.Delivery == "pending" {
				return message
			}
			return nil
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
	if s.Draft == "" && len(s.DraftImages) == 0 {
		s.Draft = message.Text
		s.DraftImages = slices.Clone(message.Images)
		s.Notice = "本条消息未发送，内容已恢复到输入框，请重新发送。"
	} else {
		s.Notice = "本条消息未发送，请重新编辑后发送。"
	}
}
