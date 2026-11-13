import json
import os
from pathlib import Path
import sys
import uuid


def emit(value):
    print(json.dumps(value), flush=True)


args = sys.argv[1:]
session_id = args[args.index("--resume") + 1] if "--resume" in args else args[args.index("--session-id") + 1]
directory = Path(os.environ["CLAUDE_CONFIG_DIR"]) / "projects" / "fixture"
directory.mkdir(parents=True, exist_ok=True)
transcript = directory / (session_id + ".jsonl")
parent = None
if transcript.exists():
    records = [json.loads(line) for line in transcript.read_text().splitlines()]
    parent = records[-1]["uuid"]
with open("process-starts", "a") as file:
    file.write(str(os.getpid()) + "\n")
with open("spawn-args", "a") as file:
    file.write(" ".join(args) + "\n")
prompt = None
user_id = None
permission_mode = "default"
settings = Path(os.environ["CLAUDE_CONFIG_DIR"]) / "settings.json"
if settings.exists():
    permission_mode = json.loads(settings.read_text()).get("permissions", {}).get("defaultMode", "default")


def record(value):
    global parent
    value["parentUuid"] = parent
    parent = value["uuid"]
    with transcript.open("a") as file:
        file.write(json.dumps(value) + "\n")


def complete(status="completed"):
    value = {"type": "assistant", "uuid": str(uuid.uuid4()), "session_id": session_id,
             "message": {"id": str(uuid.uuid4()), "role": "assistant", "content": [{"type": "text", "text": prompt}]}}
    record(value)
    emit(value)
    emit({"type": "result", "subtype": "success" if status == "completed" else "error_during_execution",
          "is_error": status != "completed", "terminal_reason": status, "session_id": session_id})


for line in sys.stdin:
    value = json.loads(line)
    if value["type"] == "control_request":
        subtype = value["request"]["subtype"]
        if subtype == "set_permission_mode":
            target = value["request"]["mode"]
            if Path("reject-plan-mode").exists():
                emit({"type": "control_response", "response": {"subtype": "error", "request_id": value["request_id"], "error": "mode rejected"}})
                continue
            permission_mode = target
            with open("mode-controls", "a") as file:
                file.write(target + "\n")
        emit({"type": "control_response", "response": {"subtype": "success", "request_id": value["request_id"],
              "response": {"current_permission_mode": permission_mode}}})
        if subtype == "interrupt":
            complete("aborted_streaming")
    elif value["type"] == "user":
        user_id = value["uuid"]
        prompt = value["message"]["content"][-1]["text"]
        with open("mode-turns", "a") as file:
            file.write(json.dumps({"mode": permission_mode, "prompt": prompt, "pid": os.getpid()}) + "\n")
        with open("turn-pids", "a") as file:
            file.write(str(os.getpid()) + "\n")
        record({"type": "user", "uuid": user_id, "message": {"content": prompt}})
        if prompt in ("permission", "question", "plan-exit"):
            tool = {"permission": "Bash", "question": "AskUserQuestion", "plan-exit": "ExitPlanMode"}[prompt]
            arguments = {"command": "printf fixture"} if prompt == "permission" else {"questions": [
                {"question": "Choose", "header": "Target", "options": [{"label": "One"}]}]}
            if prompt == "plan-exit":
                arguments = {"plan": "# 方案\n\n实现功能并验证结果"}
            emit({"type": "control_request", "request_id": "same-native-request", "request": {
                "subtype": "can_use_tool", "tool_name": tool, "tool_use_id": "tool-" + user_id, "input": arguments}})
        elif prompt == "wait":
            emit({"type": "stream_event", "session_id": session_id, "event": {
                "type": "message_start", "message": {"id": user_id}}})
        elif prompt == "exit":
            break
        elif prompt == "fail":
            complete("failed")
        else:
            complete()
    elif value["type"] == "control_response":
        if prompt == "plan-exit":
            with open("plan-decision", "w") as file:
                file.write(json.dumps({"mode": permission_mode, "decision": value["response"]["response"]}))
        complete()
