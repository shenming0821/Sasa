package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Event struct {
	Session    SessionKey
	Generation ProcessGeneration
	ID         json.RawMessage
	Method     string
	Params     json.RawMessage
	Err        error
	Truncated  bool
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("RPC %d: %s", e.Code, e.Message) }

func IsActiveWriter(err error, id string) bool {
	var rpc *RPCError
	if !errors.As(err, &rpc) {
		return false
	}
	if rpc.Code == -32600 && rpc.Message == "thread "+id+" already has an active writer" {
		return true
	}
	// Devin ACP reports a live lock holder as session_locked.
	return rpc.Code == -32015 && strings.Contains(rpc.Message, "'"+id+"'") &&
		strings.Contains(rpc.Message, "already open in another process")
}
