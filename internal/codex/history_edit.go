package codex

import (
	"context"
	"fmt"
	"reflect"

	"github.com/shenming0821/Sasa/internal/agent"
)

func (c *Client) EditHistory(ctx context.Context, threadID, turnID, messageID string) (agent.HistoryEdit, error) {
	var result agent.HistoryEdit
	turns, err := agent.ReadHistory(ctx, c, threadID)
	if err != nil {
		return result, err
	}
	boundary, err := agent.EditBoundary(turns, turnID, messageID)
	if err != nil {
		return result, err
	}
	result.Skills, err = agent.EditSkills(turns[boundary], messageID)
	if err != nil {
		return result, err
	}
	err = c.Call(ctx, "thread/fork", map[string]any{
		"threadId": threadID, "beforeTurnId": turnID, "excludeTurns": true, "deferGoalContinuation": true,
	}, &result.Thread)
	if err != nil {
		return result, err
	}
	id := result.Thread.ThreadMeta.ID
	if id == "" || id == threadID || result.Thread.Cwd == "" {
		return result, fmt.Errorf("历史编辑返回的会话标识或目录不完整")
	}
	result.Turns, err = agent.ReadHistory(ctx, c, id)
	if err != nil {
		return result, err
	}
	if len(result.Turns) != boundary {
		return result, fmt.Errorf("原生历史截断范围不符合预期，未发送修改内容")
	}
	for index, turn := range result.Turns {
		if turn.ID != turns[index].ID || !reflect.DeepEqual(turn.Items, turns[index].Items) {
			return result, fmt.Errorf("原生历史前缀不一致，未发送修改内容")
		}
	}
	return result, nil
}
