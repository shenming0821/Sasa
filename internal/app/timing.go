package app

import "time"

type TurnTiming struct {
	Started  time.Time
	Finished time.Time
}

func (s *State) BindTiming(turnID string) {
	for index := len(s.Messages) - 1; index >= 0; index-- {
		if message := s.Messages[index]; message.Role == "user" {
			if message.Timing != nil && message.Timing.Finished.IsZero() && message.TurnID == "" {
				message.TurnID = turnID
			}
			return
		}
	}
}

func (s *State) FinishTiming() {
	for index := len(s.Messages) - 1; index >= 0; index-- {
		message := s.Messages[index]
		if message.Role == "user" {
			if message.Timing != nil && message.Timing.Finished.IsZero() {
				message.Timing.Finished = time.Now()
			}
			return
		}
	}
}

func (s *State) RestoreTimings(timings map[string]TurnTiming) {
	for _, message := range s.Messages {
		if timing, ok := timings[message.TurnID]; ok && message.Role == "user" {
			message.Timing = &timing
		}
	}
}
