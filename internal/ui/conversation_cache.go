package ui

import "github.com/shenming0821/Sasa/internal/app"

type conversationCache struct {
	projectionKeys    []conversationKey
	projectedRows     []conversationRow
	projectedMessages []*app.Message
	navigationEntries []conversationEntry
}

type conversationKey struct {
	message                     *app.Message
	role, turn, kind, status    string
	parent                      string
	empty, interaction, changes bool
	finished, expanded          bool
}

func messageConversationKey(message *app.Message) conversationKey {
	key := conversationKey{
		message: message, role: message.Role, turn: message.TurnID,
		empty: message.Role == "assistant" && message.Text == "", interaction: message.Interaction != nil,
	}
	if message.Tool != nil {
		key.kind, key.status = message.Tool.Kind, message.Tool.Status
		key.parent, key.changes = message.Tool.ParentToolID, len(message.Tool.Changes) > 0
	}
	return key
}

func (s *session) conversationUnchanged() bool {
	unchanged := len(s.projectionKeys) == len(s.State.Messages)
	if cap(s.projectionKeys) < len(s.State.Messages) {
		s.projectionKeys = make([]conversationKey, len(s.State.Messages))
	} else {
		clear(s.projectionKeys[len(s.State.Messages):cap(s.projectionKeys)])
		s.projectionKeys = s.projectionKeys[:len(s.State.Messages)]
	}
	for index, message := range s.State.Messages {
		key := messageConversationKey(message)
		if message.Role == "user" {
			key.finished = message.Timing != nil && !message.Timing.Finished.IsZero()
			key.expanded = s.expandedProcesses[message.ID] || s.processProgress[message.ID] > 0
		}
		if s.projectionKeys[index] != key {
			unchanged = false
			s.projectionKeys[index] = key
		}
	}
	return unchanged
}
