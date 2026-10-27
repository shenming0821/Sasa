package agent

import (
	"encoding/json"
	"fmt"
)

type ApprovalDecision string

const (
	Allow ApprovalDecision = "allow"
	Deny  ApprovalDecision = "deny"
)

type RequestKey struct {
	Session    SessionKey
	Generation ProcessGeneration
	RequestID  string
}

// RuleSuggestion preserves adapter-provided rules without inferring their scope.
// A session rule can be selected only after that exact mapping is verified.
type RuleSuggestion struct {
	ID          string
	Description string
	Destination string
	Rule        json.RawMessage
	Verified    bool
}

type ApprovalIntent struct {
	Request      RequestKey
	Decision     ApprovalDecision
	Feedback     string
	Stop         bool
	Rule         *RuleSuggestion
	UpdatedInput json.RawMessage
}

func (i ApprovalIntent) Validate() error {
	if i.Request.RequestID == "" || i.Request.Session.SessionID == "" || i.Request.Generation == 0 {
		return fmt.Errorf("审批缺少会话、进程代次或请求标识")
	}
	if err := i.Request.Session.Agent.Validate(); err != nil {
		return err
	}
	if i.Decision != Allow && i.Decision != Deny {
		return fmt.Errorf("审批只能选择本次允许或拒绝")
	}
	if i.Rule != nil {
		if i.Decision != Allow || !i.Rule.Verified || i.Rule.Destination != "session" || i.Rule.ID == "" {
			return fmt.Errorf("仅允许附加已验证的会话规则建议")
		}
		var rule map[string]json.RawMessage
		if json.Unmarshal(i.Rule.Rule, &rule) != nil || len(rule) == 0 {
			return fmt.Errorf("规则建议必须保留原始规则对象")
		}
		var action string
		_ = json.Unmarshal(rule["type"], &action)
		if action == "setMode" {
			return fmt.Errorf("权限模式必须通过独立设置修改")
		}
	}
	if len(i.UpdatedInput) > 0 && !json.Valid(i.UpdatedInput) {
		return fmt.Errorf("修改后的工具输入不是有效 JSON")
	}
	return nil
}

// PermissionModeChange is a separate explicit setting, never an approval scope.
type PermissionModeChange struct {
	Session SessionKey
	Mode    string
}
