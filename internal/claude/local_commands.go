package claude

import (
	"encoding/json"
	"strings"
)

func localCommandOutput(entry transcriptEntry, saved checkpoint) (string, bool) {
	command := strings.TrimSpace(saved.Command)
	fields := strings.Fields(command)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return "", false
	}
	if entry.Type != "system" || entry.Subtype != "local_command" || entry.Level != "info" || entry.Meta || entry.Sidechain || entry.Team != "" || entry.Parent != saved.UserUUID {
		return "", false
	}
	if entry.CommandRun.Command != strings.TrimPrefix(fields[0], "/") || strings.TrimSpace(entry.CommandRun.Args) != strings.TrimSpace(strings.TrimPrefix(command, fields[0])) {
		return "", false
	}
	const prefix, suffix = "<local-command-stdout>", "</local-command-stdout>"
	var content string
	if json.Unmarshal(entry.Content, &content) != nil || !strings.HasPrefix(content, prefix) || !strings.HasSuffix(content, suffix) {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(content, prefix), suffix), true
}

func (c *Client) localCommandCompletion(s session, expectedDigest string) (string, bool) {
	if len(s.Turns) == 0 {
		return "", false
	}
	raw, err := c.transcript(s.ID)
	if err != nil || expectedDigest != "" && digest(raw) != expectedDigest {
		return "", false
	}
	chain, err := conversation(raw, s.ID)
	if err != nil || len(chain) == 0 {
		return "", false
	}
	saved, entry := s.Turns[len(s.Turns)-1], chain[len(chain)-1]
	if saved.LastUUID != "" && saved.LastUUID != entry.UUID {
		return "", false
	}
	if _, ok := localCommandOutput(entry, saved); !ok {
		return "", false
	}
	return entry.UUID, true
}
