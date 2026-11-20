package ui

import (
	"fmt"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/app"
)

func (s *session) animateProcesses(c *native.Context) map[string]native.Element {
	motions := make(map[string]native.Element)
	progress := make(map[string]float32)
	moving := s.disclosureMoving
	s.disclosureMoving = false
	for _, message := range s.State.Messages {
		if message.Role != "user" || message.Timing == nil {
			continue
		}
		open := message.Timing.Finished.IsZero() || s.expandedProcesses[message.ID]
		target := float32(0)
		if open {
			target = 1
		}
		motion := native.Box(c.Key("turn-process-motion:"+message.ID)).Size(0, 0)
		motions[message.ID] = motion
		progress[message.ID] = motion.AnimateTo("open", target, disclosureAnimation)
		previous, exists := s.processProgress[message.ID]
		if !message.Timing.Finished.IsZero() && (progress[message.ID] != target || exists && previous != target) {
			moving = true
		}
	}
	s.processProgress = progress
	if s.processScrollHold {
		if !moving {
			s.processScrollHold = false
		}
		c.AnimationFrame()
	}
	return motions
}

func (s *session) processScale(id string) float32 {
	if progress, exists := s.processProgress[id]; exists {
		return progress
	}
	if s.expandedProcesses[id] {
		return 1
	}
	return 0
}

func timingText(timing *app.TurnTiming, now time.Time) string {
	if timing == nil || timing.Started.IsZero() {
		return ""
	}
	prefix, end := "已处理 ", now
	if !timing.Finished.IsZero() {
		prefix, end = "用时 ", timing.Finished
	}
	seconds := max(0, int(end.Sub(timing.Started)/time.Second))
	if seconds >= 3600 {
		return fmt.Sprintf("%s%d小时 %d分钟 %d秒", prefix, seconds/3600, seconds/60%60, seconds%60)
	}
	if seconds >= 60 {
		return fmt.Sprintf("%s%d分钟 %d秒", prefix, seconds/60, seconds%60)
	}
	return fmt.Sprintf("%s%d秒", prefix, seconds)
}

func (s *session) hasTurnProcess(question *app.Message) bool {
	inTurn, hasReply := false, false
	for _, message := range s.State.Messages {
		if message.Role == "user" {
			if inTurn {
				if question.TurnID != "" && message.TurnID == question.TurnID {
					continue
				}
				break
			}
			inTurn = message == question
			continue
		}
		if !inTurn || message.ContextCompaction {
			continue
		}
		if message.Role != "assistant" {
			if !changeRecord(message) {
				return true
			}
			continue
		}
		// Empty assistant placeholders are skipped by conversationRows; the
		// final nonempty reply stays visible even when the process is folded.
		if message.Text != "" {
			if hasReply {
				return true
			}
			hasReply = true
		}
	}
	return false
}

func (s *session) turnTiming(c *native.Context, message *app.Message, motions ...native.Element) {
	timing := message.Timing
	if text := timingText(timing, time.Now()); text != "" {
		native.Column(c).FillWidth().Gap(10).Children(func() {
			native.Row(c).Gap(8).AlignItems(native.Center).Children(func() {
				if !timing.Finished.IsZero() && s.hasTurnProcess(message) {
					label := "展开"
					if s.expandedProcesses[message.ID] {
						label = "收起"
					}
					button := native.ButtonBase(c).Label(label + "执行过程 " + message.ID).
						Role(native.RoleDisclosure).Expanded(s.expandedProcesses[message.ID]).Padding(0).Gap(8).AlignItems(native.Center)
					if button.Clicked() {
						s.processScrollHold = true
						s.list.FollowEnd = false
						if s.expandedProcesses == nil {
							s.expandedProcesses = make(map[string]bool)
						}
						s.expandedProcesses[message.ID] = !s.expandedProcesses[message.ID]
						if len(motions) > 0 && motions[0].Valid() {
							s.processProgress[message.ID] = animateDisclosure(motions[0], "open", s.expandedProcesses[message.ID])
						}
						label = "展开"
						if s.expandedProcesses[message.ID] {
							label = "收起"
						}
						button.Label(label + "执行过程 " + message.ID).Expanded(s.expandedProcesses[message.ID])
						c.AnimationFrame()
					}
					color := c.Theme().TextMuted
					if button.Hovered() || button.FocusVisible() {
						color = c.Theme().Text
					}
					button.TextColor(color).Children(func() {
						native.Text(c, text).FontSize(13).SingleLine()
						native.Icon(c, chevron).Label("执行过程箭头 "+message.ID).Size(16, 16).
							Rotate(-90*(1-s.processScale(message.ID))).Translate(0, 0.5)
					})

				} else {
					native.Text(c, text).FontSize(13).TextColor(c.Theme().TextMuted).SingleLine()
				}
			})
			native.Box(c).FillWidth().Height(1).Background(c.Theme().Border.Alpha(.6))
		})
	}
}

func (s *session) visibleConversationMessages() []*app.Message {
	var visible []*app.Message
	for start := 0; start < len(s.State.Messages); {
		message := s.State.Messages[start]
		if message.Role != "user" {
			visible = append(visible, message)
			start++
			continue
		}
		end := start + 1
		for end < len(s.State.Messages) && (s.State.Messages[end].Role != "user" || message.TurnID != "" && s.State.Messages[end].TurnID == message.TurnID) {
			end++
		}
		if message.Timing == nil || message.Timing.Finished.IsZero() {
			visible = append(visible, s.State.Messages[start:end]...)
			start = end
			continue
		}
		// Finished turns pin the final reply after the process, matching the
		// collapsed view; tool records otherwise expand below the answer.
		folded := !s.expandedProcesses[message.ID] && s.processProgress[message.ID] <= 0
		foreignTurn := false
		for _, candidate := range s.State.Messages[start+1 : end] {
			if message.TurnID != "" && candidate.Role != "user" && candidate.TurnID != "" && candidate.TurnID != message.TurnID {
				foreignTurn = true
				break
			}
		}
		if foreignTurn {
			visible = append(visible, s.State.Messages[start:end]...)
			start = end
			continue
		}
		visible = append(visible, message)
		resultIndex := -1
		for index, candidate := range s.State.Messages[start+1 : end] {
			if candidate.Role == "assistant" && !candidate.ContextCompaction && candidate.Text != "" {
				resultIndex = index
			}
		}
		var result *app.Message
		for index, candidate := range s.State.Messages[start+1 : end] {
			if index == resultIndex {
				result = candidate
				continue
			}
			if !folded || candidate.ContextCompaction || changeRecord(candidate) || candidate.Role == "user" {
				visible = append(visible, candidate)
			}
		}
		if result != nil {
			visible = append(visible, result)
		}
		start = end
	}
	return visible
}

func (a *App) conversationTimings(rows []conversationRow) map[int]*app.TurnTiming {
	timings := make(map[int]*app.TurnTiming)
	current := -1
	for index, row := range rows {
		if row.message.Role == "user" && row.message.Timing != nil {
			current = index
			if timing := row.message.Timing; timing != nil && !timing.Finished.IsZero() {
				timings[index] = timing
			}
		}
	}
	if a.State.Busy && current >= 0 {
		if timing := rows[current].message.Timing; timing != nil && timing.Finished.IsZero() {
			timings[current] = timing
		}
	}
	return timings
}
