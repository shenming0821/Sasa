package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEditHistoryUsesNativeBoundaryAndPagination(t *testing.T) {
	directory := t.TempDir()
	turns := []HistoryTurn{}
	for index := 0; index < 25; index++ {
		turns = append(turns, HistoryTurn{ID: fmt.Sprintf("turn-%d", index), Status: "completed", ItemsView: "full", Items: []json.RawMessage{json.RawMessage(fmt.Sprintf(`{"id":"user-%d","type":"userMessage","content":[{"type":"text","text":"question %d"}]}`, index, index))}})
	}
	raw, _ := json.Marshal(map[string]any{"source": map[string]any{"cwd": directory, "turns": turns}})
	if err := os.WriteFile(filepath.Join(directory, "native-history.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	script, _ := filepath.Abs("testdata/p3_server.py")
	client, err := startProcess("/usr/bin/python3", []string{script, "--fixture-dir", directory}, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, boundary := range []int{0, 1, 24} {
		result, err := client.EditHistory(ctx, "source", fmt.Sprintf("turn-%d", boundary), fmt.Sprintf("user-%d", boundary))
		if err != nil || len(result.Turns) != boundary || result.Thread.ThreadMeta.ID == "source" {
			t.Fatalf("boundary=%d result=%+v error=%v", boundary, result, err)
		}
	}
	if _, err := client.EditHistory(ctx, "source", "turn-1", "wrong-message"); err == nil {
		t.Fatal("mismatched message was editable")
	}
	page, err := client.ListTurns(ctx, "source", nil)
	if err != nil || len(page.Data) != 20 || page.NextCursor == nil {
		t.Fatal("original history changed", err)
	}
}
