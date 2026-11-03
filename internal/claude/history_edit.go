package claude

import (
	"context"
	"fmt"
	"slices"

	"github.com/shenming0821/Fuyu/internal/agent"
)

func (c *Client) historyChain(s session) ([]transcriptEntry, string, error) {
	raw, err := c.transcript(s.ID)
	if err == nil {
		chain, err := conversation(raw, s.ID)
		return chain, digest(raw), err
	}
	if s.ResumeSource == "" {
		return nil, "", err
	}
	raw, err = c.transcript(s.ResumeSource)
	if err != nil {
		return nil, "", err
	}
	if digest(raw) != s.ResumeDigest {
		return nil, "", fmt.Errorf("编辑来源历史已变化，请重新核验会话")
	}
	chain, err := conversation(raw, s.ResumeSource)
	if err != nil {
		return nil, "", err
	}
	for index, entry := range chain {
		if entry.UUID == s.ResumeAt {
			return chain[:index+1], s.ResumeDigest, nil
		}
	}
	return nil, "", fmt.Errorf("编辑来源历史缺少恢复边界")
}

func (c *Client) EditHistory(ctx context.Context, threadID, turnID, messageID string) (agent.HistoryEdit, error) {
	var result agent.HistoryEdit
	if _, err := c.ReleaseIdleThread(ctx, threadID); err != nil {
		return result, err
	}
	source, err := c.sessionCopy(threadID)
	if err != nil {
		return result, err
	}
	if source.run != nil || source.idle != nil || source.retiring != nil {
		return result, fmt.Errorf("Claude 会话仍在执行任务，请稍后编辑")
	}
	turns, hash, err := c.history(source)
	if err != nil {
		return result, err
	}
	if hash != source.Digest {
		return result, fmt.Errorf("Claude 历史已变化，请重新加载会话")
	}
	chain, _, err := c.historyChain(source)
	if err != nil {
		return result, err
	}
	position := -1
	for index, entry := range chain {
		if entry.Subtype == "compact_boundary" || entry.Summary || entry.RepeatsSummary {
			return result, fmt.Errorf("此会话已压缩，暂时无法可靠定位编辑边界")
		}
		if entry.UUID == messageID || entry.Type == "user" && entry.PromptID == messageID {
			position = index
		}
	}
	if position < 0 {
		return result, fmt.Errorf("Claude 原生历史未找到此消息")
	}
	messageID = chain[position].UUID
	boundary, err := agent.EditBoundary(turns, turnID, messageID)
	if err != nil {
		return result, err
	}
	if boundary == 0 {
		fresh, err := c.StartThread(ctx, source.Cwd)
		if err != nil {
			return result, err
		}
		result.Thread, err = c.ResumeThread(ctx, fresh.Thread.ID)
		return result, err
	}
	resumeAt := ""
	for index := position - 1; index >= 0; index-- {
		entry := chain[index]
		if entry.Type == "assistant" && !entry.Meta && !entry.Sidechain {
			resumeAt = entry.UUID
			break
		}
		if entry.Type != "attachment" && entry.Type != "progress" && !(entry.Type == "system" && entry.Subtype == "stop_hook_summary") {
			return result, fmt.Errorf("编辑边界之间存在无法归属的消息")
		}
	}
	if resumeAt == "" {
		return result, fmt.Errorf("此消息之前没有完整的原生回答边界，暂时无法编辑")
	}
	lastCheckpoint := -1
	for index, saved := range source.Turns {
		if saved.ID == turnID && (saved.UserUUID == messageID || saved.UserUUID == chain[position].PromptID) {
			lastCheckpoint = index
			break
		}
	}
	if lastCheckpoint <= 0 {
		return result, fmt.Errorf("Claude 编辑消息与任务检查点不匹配")
	}
	id, err := uuid()
	if err != nil {
		return result, err
	}
	branch := &session{
		ID: id, Cwd: source.Cwd, Title: source.Title, Model: source.Model, EffectiveModel: source.EffectiveModel, Version: source.Version,
		Turns: slices.Clone(source.Turns[:lastCheckpoint]), Digest: hash,
		ResumeSource: source.ID, ResumeAt: resumeAt, ResumeDigest: hash,
	}
	if source.ResumeSource != "" {
		if _, err := c.transcript(source.ID); err != nil {
			branch.ResumeSource, branch.ResumeDigest = source.ResumeSource, source.ResumeDigest
		}
	}
	c.mu.Lock()
	if ctx.Err() != nil || c.closed || c.failed || c.sessions[threadID].Generation != source.Generation || c.sessions[threadID].run != nil {
		c.mu.Unlock()
		return result, fmt.Errorf("Claude 编辑期间会话状态已变化")
	}
	err = c.save(branch)
	if err == nil {
		c.sessions[id] = branch
	}
	c.mu.Unlock()
	if err != nil {
		return result, err
	}
	result.Thread, err = c.ResumeThread(ctx, id)
	if err != nil {
		return result, err
	}
	result.Turns, _, err = c.history(*branch)
	if err == nil && len(result.Turns) != boundary {
		err = fmt.Errorf("Claude 恢复前缀与编辑边界不一致")
	}
	return result, err
}
