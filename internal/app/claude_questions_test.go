package app

import (
	"encoding/json"
	"testing"

	"github.com/shenming0821/Fuyu/internal/agent"
)

func TestMultipleQuestionSelectionsAndOtherAnswer(t *testing.T) {
	question := agent.Question{ID: "targets", Header: "平台", MultiSelect: true, IsOther: true, Options: []agent.Option{{Label: "Mac"}, {Label: "Linux"}}}
	request := &Interaction{Status: "待处理", Request: agent.Interaction{Method: "item/tool/requestUserInput", Questions: []agent.Question{question}}, Answers: []string{""}}
	if _, err := request.Submit(nil); err == nil || request.InvalidQuestion != "targets" {
		t.Fatal("empty multi-select accepted")
	}
	request.ToggleAnswer(0, "Mac")
	request.ToggleAnswer(0, "Linux")
	request.ToggleAnswer(0, "Mac")
	if request.AnswerSelected(question, "Mac") || !request.AnswerSelected(question, "Linux") {
		t.Fatal("selection toggle failed")
	}
	request.Answers[0] = "FreeBSD"
	response, err := request.Submit(nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(response)
	if string(raw) != `{"answers":{"targets":{"answers":["Linux","FreeBSD"]}}}` {
		t.Fatal("multi-select and free text did not reach native answers", string(raw))
	}
	if len(request.Selections) != 0 || request.Answers[0] != "" {
		t.Fatal("submitted answers retained")
	}
}

func TestRuntimeDetailsUseNativeModelAndVersion(t *testing.T) {
	state := New("/project")
	state.Agent, state.ThreadID, state.Model = agent.Claude, "current", "default"
	state.Apply(agent.Event{Method: "fuyu/runtime", Params: json.RawMessage(`{"threadId":"other","model":"wrong","version":"wrong"}`)})
	if state.Model != "default" || state.RuntimeVersion != "" {
		t.Fatal("foreign metadata changed runtime details")
	}
	state.Apply(agent.Event{Method: "fuyu/runtime", Params: json.RawMessage(`{"threadId":"current","model":"provider/model","version":"2.1.307"}`)})
	if state.Model != "provider/model" || state.RuntimeVersion != "2.1.307" {
		t.Fatal("runtime details did not retain native values")
	}
}
