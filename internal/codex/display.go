package codex

import "github.com/shenming0821/Sasa/internal/agent"

const MaxToolOutputBytes = agent.MaxToolOutputBytes

func displayEvent(e Event) Event { return e.ForDisplay() }
