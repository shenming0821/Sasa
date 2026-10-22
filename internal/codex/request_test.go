package codex

import (
	"encoding/json"
	"testing"
)

func TestAvailableDecisionsPreserveNativeValuesAndScope(t *testing.T) {
	raw := []json.RawMessage{json.RawMessage(`"decline"`), json.RawMessage(`{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":["go","test"]}}`), json.RawMessage(`{"applyNetworkPolicyAmendment":{"network_policy_amendment":{"host":"example.test","action":"deny"}}}`)}
	r := Interaction{Method: "item/commandExecution/requestApproval", AvailableDecisions: raw}
	choices := r.Decisions()
	if len(choices) != 3 {
		t.Fatal(choices)
	}
	for i, choice := range choices {
		if string(choice.Value) != string(raw[i]) || choice.Label == "" {
			t.Fatal(choice)
		}
	}
	if choices[0].Label != "拒绝操作" {
		t.Fatal("did not preserve native order")
	}
	r.AvailableDecisions = []json.RawMessage{}
	if len(r.Decisions()) != 0 {
		t.Fatal("invented actions for explicitly empty decision list")
	}
}

func TestResolvedAndCompletedExpireExactNativeIDs(t *testing.T) {
	c := &Client{server: map[string]*serverRequest{
		`1`:   {thread: "thread", turn: "turn"},
		`"1"`: {thread: "other", turn: "turn"},
		`2`:   {thread: "thread", turn: "next"},
	}}
	c.expireRequests(envelope{Method: "serverRequest/resolved", Params: json.RawMessage(`{"threadId":"thread","requestId":"1"}`)})
	if len(c.server) != 3 {
		t.Fatal("crossed string/numeric or thread identity")
	}
	c.expireRequests(envelope{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn"}}`)})
	if len(c.server) != 2 || c.server[`2`] == nil {
		t.Fatal(c.server)
	}
}

func TestRequestIdentityMatchesNativeSchema(t *testing.T) {
	for _, id := range []string{`null`, `{}`, `1.5`, `9223372036854775808`} {
		_, err := DecodeInteraction(Event{ID: json.RawMessage(id), Method: "item/fileChange/requestApproval",
			Params: json.RawMessage(`{"threadId":"thread","turnId":"turn","itemId":"file"}`)})
		if err == nil {
			t.Fatalf("invalid native request id accepted: %s", id)
		}
	}
}
