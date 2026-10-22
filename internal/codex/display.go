package codex

import (
	"encoding/json"
	"unicode/utf8"
)

const MaxToolOutputBytes = 256 * 1024

func (e Event) ForDisplay() Event { return displayEvent(e) }

// A final tool item can repeat megabytes of accumulated output. Keep its
// identity/status authoritative while limiting display-only fields before it
// enters the bounded queue. Oversized approval requests are rejected instead.
func displayEvent(e Event) Event {
	if (e.Method != "item/started" && e.Method != "item/completed") || len(e.Params) < MaxToolOutputBytes {
		return e
	}
	var p map[string]json.RawMessage
	if json.Unmarshal(e.Params, &p) != nil {
		return e
	}
	var item map[string]json.RawMessage
	if json.Unmarshal(p["item"], &item) != nil {
		return e
	}
	changed := false
	for _, field := range []string{"aggregatedOutput", "command", "text"} {
		raw := item[field]
		limit := MaxToolOutputBytes
		if field == "text" {
			limit = 2 * 1024 * 1024
		}
		var text string
		if len(raw) > limit && json.Unmarshal(raw, &text) == nil && len(text) > limit {
			n := limit
			for !utf8.ValidString(text[:n]) {
				n--
			}
			item[field], _ = json.Marshal(text[:n])
			changed = true
		}
	}
	for _, field := range []string{"arguments", "result", "contentItems", "error"} {
		if len(item[field]) > MaxToolOutputBytes {
			item[field] = json.RawMessage(`"[内容超过显示上限，请核验原生记录]"`)
			changed = true
		}
	}
	if raw := item["changes"]; len(raw) > MaxToolOutputBytes {
		var changes []map[string]json.RawMessage
		if json.Unmarshal(raw, &changes) == nil {
			remaining := MaxToolOutputBytes
			for _, change := range changes {
				var diff string
				if json.Unmarshal(change["diff"], &diff) == nil && len(diff) > remaining {
					n := remaining
					for n > 0 && !utf8.ValidString(diff[:n]) {
						n--
					}
					diff = diff[:n]
					changed = true
				}
				remaining -= len(diff)
				change["diff"], _ = json.Marshal(diff)
			}
			item["changes"], _ = json.Marshal(changes)
		}
	}
	if changed {
		p["item"], _ = json.Marshal(item)
		e.Params, _ = json.Marshal(p)
		e.Truncated = true
	}
	return e
}
