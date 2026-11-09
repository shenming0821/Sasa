package devin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shenming0821/Fuyu/internal/agent"
)

// sessionState persists the application turn ids issued for a Devin session so
// recovery can match ACP replay turns against the identity the UI saved. The
// user text prefix distinguishes same-session prompts submitted elsewhere.
type sessionState struct {
	dir   string
	ID    string      `json:"id"`
	Cwd   string      `json:"cwd"`
	Turns []stateTurn `json:"turns"`
}

type stateTurn struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	User   string `json:"user"`
}

const userPrefixLimit = 160

func (c *Client) loadState(id string) *sessionState {
	state := &sessionState{dir: c.stateDir, ID: id}
	raw, err := os.ReadFile(filepath.Join(c.stateDir, id+".json"))
	if err != nil {
		return state
	}
	if len(raw) > 4<<20 || json.Unmarshal(raw, state) != nil || state.ID != id {
		return &sessionState{dir: c.stateDir, ID: id}
	}
	state.dir = c.stateDir
	return state
}

func (c *Client) saveState(s *sessionState) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(s.dir, ".turn-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(s.dir, s.ID+".json"))
}

func userPrefix(text string) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) > userPrefixLimit {
		runes = runes[:userPrefixLimit]
	}
	return string(runes)
}

func (s *sessionState) appendTurn(id, user string) {
	s.Turns = append(s.Turns, stateTurn{ID: id, Status: "inProgress", User: userPrefix(user)})
}

func (s *sessionState) dropTurn(id string) {
	for i := len(s.Turns) - 1; i >= 0; i-- {
		if s.Turns[i].ID == id {
			s.Turns = append(s.Turns[:i], s.Turns[i+1:]...)
			return
		}
	}
}

func (s *sessionState) finishTurn(id, status string) {
	for i := len(s.Turns) - 1; i >= 0; i-- {
		if s.Turns[i].ID == id {
			s.Turns[i].Status = status
			return
		}
	}
}

// assignTurnIDs overlays saved turn ids onto replayed turns whose user text
// matches, in order. Unmatched replay turns receive synthetic ids.
func (s *sessionState) assignTurnIDs(turns []agent.HistoryTurn, users []string) {
	next := 0
	for i := range turns {
		turns[i].ID = fmt.Sprintf("turn-%d", i+1)
	}
	for _, saved := range s.Turns {
		for j := next; j < len(turns); j++ {
			if users[j] == saved.User {
				turns[j].ID = saved.ID
				next = j + 1
				break
			}
		}
	}
}
