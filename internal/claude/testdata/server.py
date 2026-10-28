import json
import os
import sys


def emit(message):
    print(json.dumps(message), flush=True)


session = None
pending = None
terminal_reason = "aborted_tools"
for line in sys.stdin:
    message = json.loads(line)
    if message["type"] == "control_request":
        request = message["request"]
        response = {"type": "control_response", "response": {
            "subtype": "success", "request_id": message["request_id"],
            "response": {"current_permission_mode": "default", "configHome": os.environ.get("CLAUDE_CONFIG_DIR"), "models": [{"value": "default", "displayName": "Default"}, {"value": "opus", "resolvedModel": "fixture/model", "displayName": "Fixture"}]}}}
        emit(response)
        if request["subtype"] == "interrupt":
            emit({"type": "result", "subtype": "error_during_execution", "is_error": True,
                  "terminal_reason": terminal_reason, "session_id": session})
    elif message["type"] == "user":
        session = message["session_id"]
        text = message["message"]["content"][-1]["text"]
        terminal_reason = "aborted_streaming" if text.endswith("streaming") else "aborted_tools"
        if text == "bad-json":
            print("{broken", flush=True)
            break
        if text == "unexpected-exit":
            break
        if text == "truncated-json":
            sys.stdout.write('{"type":"result"')
            sys.stdout.flush()
            break
        if text == "overlong":
            sys.stdout.write('{"type":"unknown","text":"' + 'x' * (16 * 1024 * 1024) + '"}\n')
            sys.stdout.flush()
            break
        if text == "unknown-result":
            emit({"type": "result", "subtype": "future_terminal", "session_id": session})
            continue
        if text == "fragmented":
            value = json.dumps({"type": "result", "subtype": "success", "is_error": False,
                                "terminal_reason": "completed", "session_id": session}) + "\n"
            for part in value:
                sys.stdout.write(part)
                sys.stdout.flush()
            continue
        if text == "cancel-permission":
            permission = {"type": "control_request", "request_id": "cancelled-1",
                          "request": {"subtype": "can_use_tool", "tool_name": "Bash",
                                      "tool_use_id": "tool-1", "input": {"command": "printf fixture"}}}
            emit(permission)
            emit(permission)
            emit({"type": "control_cancel_request", "request_id": "cancelled-1"})
            emit(permission)
            emit({"type": "result", "subtype": "success", "is_error": False,
                  "terminal_reason": "completed", "session_id": session})
            continue
        if text in ("failed-result", "failed-result-streaming"):
            emit({"type": "result", "subtype": "error_during_execution", "is_error": True,
                  "terminal_reason": terminal_reason, "session_id": session, "errors": ["fixture failure"]})
        else:
            pending = "permission-1"
            tool = "Bash"
            arguments = {"command": "printf fixture"}
            if text == "question":
                tool = "AskUserQuestion"
                arguments = {"questions": [{"header": "Targets", "question": "Which targets?",
                    "multiSelect": True, "options": [{"label": "Mac", "description": "Desktop"},
                    {"label": "Linux", "description": "Server"}]}]}
            emit({"type": "control_request", "request_id": pending,
                  "request": {"subtype": "can_use_tool", "tool_name": tool,
                              "tool_use_id": "tool-1", "input": arguments}})
    elif message["type"] == "control_response":
        response = message["response"]["response"]
        with open("decision.json", "w") as decision:
            json.dump(response, decision)
        if response.get("behavior") == "allow":
            with open("approved.txt", "w") as proof:
                proof.write("fixture\n")
        emit({"type": "result", "subtype": "success", "is_error": False,
              "terminal_reason": "completed", "session_id": session})
