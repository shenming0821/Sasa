package codex

import "github.com/shenming0821/Fuyu/internal/agent"

const MaxToolOutputBytes = agent.MaxToolOutputBytes

func displayEvent(e Event) Event { return e.ForDisplay() }
