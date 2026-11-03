package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
)

func ReadHistory(ctx context.Context, backend HistoryBackend, threadID string) ([]HistoryTurn, error) {
	var turns []HistoryTurn
	var cursor *string
	seen := map[string]bool{}
	bytes := 0
	for {
		page, err := backend.ListTurns(ctx, threadID, cursor)
		if err != nil {
			return nil, err
		}
		for _, turn := range page.Data {
			if turn.ID == "" || seen[turn.ID] {
				return nil, fmt.Errorf("原生历史任务标识重复或缺失")
			}
			seen[turn.ID] = true
			for _, item := range turn.Items {
				bytes += len(item)
			}
		}
		turns = append(turns, page.Data...)
		if bytes > 64<<20 || len(turns) > 100000 {
			return nil, fmt.Errorf("历史超过编辑核验上限")
		}
		if page.NextCursor == nil {
			break
		}
		if len(page.Data) == 0 || cursor != nil && *cursor == *page.NextCursor {
			return nil, fmt.Errorf("原生历史分页未前进")
		}
		cursor = page.NextCursor
	}
	slices.Reverse(turns)
	return turns, nil
}

func EditBoundary(turns []HistoryTurn, turnID, messageID string) (int, error) {
	boundary := -1
	for index, turn := range turns {
		if turn.ItemsView != "" && turn.ItemsView != "full" || turn.Status != "completed" && turn.Status != "failed" && turn.Status != "interrupted" {
			return 0, fmt.Errorf("历史尚未完整核验，暂时无法编辑")
		}
		if turn.ID != turnID {
			continue
		}
		users, matched := 0, false
		for _, raw := range turn.Items {
			var item struct {
				ID      string `json:"id"`
				Type    string `json:"type"`
				Content []struct {
					Type string `json:"type"`
				} `json:"content"`
			}
			if err := json.Unmarshal(raw, &item); err != nil {
				return 0, err
			}
			if item.Type != "userMessage" {
				continue
			}
			users++
			if item.ID == messageID && messageID != "" {
				matched = true
				for _, content := range item.Content {
					if content.Type != "text" && content.Type != "image" && content.Type != "localImage" {
						return 0, fmt.Errorf("此消息包含暂不支持编辑的输入内容")
					}
				}
			}
		}
		if users != 1 || !matched || boundary != -1 {
			return 0, fmt.Errorf("无法确定此消息的独立上下文边界")
		}
		boundary = index
	}
	if boundary < 0 {
		return 0, fmt.Errorf("原生历史中未找到此消息，请重新加载会话")
	}
	return boundary, nil
}
