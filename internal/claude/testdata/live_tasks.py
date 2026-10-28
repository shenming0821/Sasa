import json
import os
from pathlib import Path
import queue
import signal
import subprocess
import sys
import tempfile
import threading
import time
import uuid


def probe(executable, mode):
    root = Path(tempfile.mkdtemp(prefix="fuyu-claude-" + mode + "-"))
    session = str(uuid.uuid4())
    args = [executable, "-p", "--verbose", "--input-format", "stream-json",
            "--output-format", "stream-json", "--include-partial-messages",
            "--permission-prompt-tool", "stdio", "--permission-mode", "default",
            "--model", "default", "--session-id", session]
    child = subprocess.Popen(args, cwd=root, stdin=subprocess.PIPE,
                             stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                             text=True, start_new_session=True)
    inbox = queue.Queue(maxsize=4096)

    def read():
        for line in child.stdout:
            inbox.put(line)
        inbox.put(None)

    threading.Thread(target=read, daemon=True).start()
    wire = open(root / "wire.jsonl", "w")

    def send(value):
        wire.write(json.dumps({"direction": "stdin", "frame": value}) + "\n")
        wire.flush()
        child.stdin.write(json.dumps(value) + "\n")
        child.stdin.flush()

    prompts = {
        "subagent": "Use the Agent tool to start exactly one general-purpose subagent in the foreground. Ask it to reply exactly FU YU joined without spaces followed by _SUBAGENT_OK, without any tools or file writes. Return the same token after it finishes. Do not write memory files or use other tools.",
        "background": "Use Bash once with run_in_background=true to run exactly: sleep 2; printf 'FUYU_BACKGROUND_OK\\n' > background-proof.txt . Then wait for that background task using TaskOutput before replying FUYU_BACKGROUND_OK. Work only in this directory. Do not write memory files or use other tools.",
        "subagent-tool": "Use Agent once in the foreground with subagent_type=general-purpose. Ask the subagent to use Bash once to run printf FUYU_CHILD_TOOL_OK in this working directory, then return its output. Do not write any files or memory. Return that exact output after the subagent completes. The parent must not run Bash itself.",
    }
    initialized = False
    result = None
    notifications = []
    tools = []
    deadline = time.monotonic() + 120
    try:
        send({"type": "control_request", "request_id": "initialize-probe",
              "request": {"subtype": "initialize", "hooks": None}})
        while time.monotonic() < deadline:
            line = inbox.get(timeout=max(0.01, deadline - time.monotonic()))
            if line is None:
                break
            frame = json.loads(line)
            wire.write(json.dumps({"direction": "stdout", "frame": frame}) + "\n")
            wire.flush()
            if frame.get("type") == "control_response" and not initialized:
                if frame["response"]["subtype"] != "success":
                    raise RuntimeError("initialize rejected")
                initialized = True
                send({"type": "user", "uuid": str(uuid.uuid4()), "session_id": session,
                      "parent_tool_use_id": None, "message": {"role": "user", "content": [{"type": "text", "text": prompts[mode]}]}})
            if frame.get("type") == "control_request":
                request = frame["request"]
                allowed = request.get("tool_name") in ({"Agent", "Task"} if mode == "subagent" else {"Agent", "Task", "Bash", "TaskOutput"})
                send({"type": "control_response", "response": {"subtype": "success",
                      "request_id": frame["request_id"], "response": {
                          "behavior": "allow" if allowed else "deny", "updatedInput": request.get("input", {}),
                          "message": "Only the requested isolated probe tools are allowed"}}})
            if frame.get("type") == "assistant":
                for block in frame.get("message", {}).get("content", []):
                    if block.get("type") == "tool_use":
                        tools.append({"id": block["id"], "name": block["name"], "parent": frame.get("parent_tool_use_id"), "background": block.get("input", {}).get("run_in_background", False)})
            if frame.get("type") == "system" and frame.get("subtype", "").startswith("task_"):
                notifications.append({key: frame.get(key) for key in ("subtype", "task_id", "tool_use_id", "status")})
            if frame.get("type") == "result":
                result = {key: frame.get(key) for key in ("subtype", "is_error", "terminal_reason")}
                child.stdin.close()
        child.wait(timeout=10)
        print(json.dumps({"mode": mode, "session": session, "exit": child.returncode,
                          "result": result, "tools": tools, "notifications": notifications,
                          "proof": (root / "background-proof.txt").read_text() if (root / "background-proof.txt").exists() else None,
                          "wire": str(root / "wire.jsonl")}), flush=True)
    finally:
        wire.close()
        try:
            os.killpg(child.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        child.wait()


for mode in (sys.argv[2:] or ("subagent", "background")):
    probe(sys.argv[1], mode)
