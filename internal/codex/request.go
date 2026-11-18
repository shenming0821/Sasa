package codex

import "github.com/shenming0821/Sasa/internal/agent"

type Option = agent.Option
type Question = agent.Question
type Interaction = agent.Interaction
type Decision = agent.Decision

func DecodeInteraction(e Event) (Interaction, error) { return agent.DecodeInteraction(e) }
