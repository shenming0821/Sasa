package agent

import (
	"encoding/json"
	"errors"
	"fmt"
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
	return rpc.Code == -32600 && rpc.Message == "thread "+id+" already has an active writer"
}
