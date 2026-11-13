package agent

import (
	"encoding/json"
	"fmt"
	"strconv"
)

type Option struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type Question struct {
	ID          string   `json:"id"`
	Header      string   `json:"header"`
	Question    string   `json:"question"`
	IsOther     bool     `json:"isOther"`
	IsSecret    bool     `json:"isSecret"`
	MultiSelect bool     `json:"multiSelect,omitempty"`
	Options     []Option `json:"options"`
}

type Interaction struct {
	ID                              json.RawMessage   `json:"-"`
	Method                          string            `json:"-"`
	ThreadID                        string            `json:"threadId"`
	TurnID                          string            `json:"turnId"`
	ItemID                          string            `json:"itemId"`
	Command                         string            `json:"command"`
	Cwd                             string            `json:"cwd"`
	Reason                          string            `json:"reason"`
	Plan                            string            `json:"plan,omitempty"`
	Kind                            string            `json:"kind"`
	GrantRoot                       string            `json:"grantRoot"`
	Permissions                     json.RawMessage   `json:"permissions"`
	AdditionalPermissions           json.RawMessage   `json:"additionalPermissions"`
	NetworkApprovalContext          json.RawMessage   `json:"networkApprovalContext"`
	AvailableDecisions              []json.RawMessage `json:"availableDecisions"`
	ProposedExecpolicyAmendment     []string          `json:"proposedExecpolicyAmendment"`
	ProposedNetworkPolicyAmendments []json.RawMessage `json:"proposedNetworkPolicyAmendments"`
	Questions                       []Question        `json:"questions"`
	IsBlocking                      bool              `json:"isBlocking"`
}

func DecodeInteraction(e Event) (Interaction, error) {
	var r Interaction
	if err := json.Unmarshal(e.Params, &r); err != nil {
		return r, err
	}
	r.ID, r.Method = e.ID, e.Method
	if len(r.ID) == 0 || r.ThreadID == "" || r.TurnID == "" || r.ItemID == "" {
		return r, fmt.Errorf("interaction lacks native identity")
	}
	if r.ID[0] == '"' {
		var id string
		if err := json.Unmarshal(r.ID, &id); err != nil {
			return r, fmt.Errorf("invalid request identity: %w", err)
		}
	} else if _, err := strconv.ParseInt(string(r.ID), 10, 64); err != nil {
		return r, fmt.Errorf("request identity must be a string or int64")
	}
	if r.Method == "item/permissions/requestApproval" {
		var permissions map[string]json.RawMessage
		if err := json.Unmarshal(r.Permissions, &permissions); err != nil || permissions == nil {
			return r, fmt.Errorf("permissions request lacks an object profile")
		}
	}
	if r.Method == "item/tool/requestUserInput" {
		var required struct {
			IsBlocking *bool `json:"isBlocking"`
		}
		if err := json.Unmarshal(e.Params, &required); err != nil || required.IsBlocking == nil {
			return r, fmt.Errorf("question request lacks isBlocking")
		}
		if len(r.Questions) == 0 {
			return r, fmt.Errorf("question list is empty")
		}
		ids := map[string]bool{}
		for _, q := range r.Questions {
			if q.ID == "" || ids[q.ID] {
				return r, fmt.Errorf("invalid question identity")
			}
			ids[q.ID] = true
		}
	}
	return r, nil
}

type Decision struct {
	Label  string
	Detail string
	Value  json.RawMessage
}

func (r Interaction) Decisions() []Decision {
	values := r.AvailableDecisions
	if values == nil {
		values = []json.RawMessage{json.RawMessage(`"accept"`), json.RawMessage(`"acceptForSession"`)}
		if r.Method == "item/commandExecution/requestApproval" {
			if len(r.ProposedExecpolicyAmendment) > 0 {
				v, _ := json.Marshal(map[string]any{"acceptWithExecpolicyAmendment": map[string]any{"execpolicy_amendment": r.ProposedExecpolicyAmendment}})
				values = append(values, v)
			}
			for _, amendment := range r.ProposedNetworkPolicyAmendments {
				v, _ := json.Marshal(map[string]any{"applyNetworkPolicyAmendment": map[string]any{"network_policy_amendment": amendment}})
				values = append(values, v)
			}
		}
		values = append(values, json.RawMessage(`"decline"`), json.RawMessage(`"cancel"`))
	}
	var out []Decision
	for _, v := range values {
		d := Decision{Value: v}
		switch string(v) {
		case `"accept"`:
			d.Label, d.Detail = "允许本次", "仅允许当前操作"
		case `"acceptForSession"`:
			d.Label, d.Detail = "本会话允许", "在当前会话中缓存此授权"
			if r.Method == "item/fileChange/requestApproval" {
				d.Detail = "请求在本会话保留文件写入授权，以 Codex 的实际结果为准"
			}
		case `"decline"`:
			d.Label, d.Detail = "拒绝操作", "拒绝本次操作，允许任务继续"
		case `"cancel"`:
			d.Label, d.Detail = "拒绝并停止", "拒绝本次操作并中断当前任务"
		default:
			var object map[string]json.RawMessage
			_ = json.Unmarshal(v, &object)
			if _, ok := object["acceptWithExecpolicyAmendment"]; ok {
				d.Label = "允许并保存命令规则"
			}
			if _, ok := object["applyNetworkPolicyAmendment"]; ok {
				d.Label = "保存网络规则"
			}
			d.Detail = string(v)
		}
		if d.Label != "" {
			out = append(out, d)
		}
	}
	return out
}
