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


def probe(executable, persistent):
    root = Path(tempfile.mkdtemp(prefix="fuyu-claude-process-"))
    session = str(uuid.uuid4())
    transcript = open(root / "wire.jsonl", "w")
    process = None
    elapsed = []
    try:
        for index in range(2):
            started = time.monotonic()
            if process is None:
                args = [executable, "-p", "--verbose", "--input-format", "stream-json",
                        "--output-format", "stream-json", "--include-partial-messages",
                        "--permission-prompt-tool", "stdio", "--permission-mode", "default",
                        "--model", "default", "--resume" if index else "--session-id", session]
                process = subprocess.Popen(args, cwd=root, stdin=subprocess.PIPE,
                    stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True,
                    start_new_session=True)
                messages = queue.Queue(maxsize=4096)

                def read_stdout(child, inbox):
                    for line in child.stdout:
                        inbox.put(line)
                    inbox.put(None)

                threading.Thread(target=read_stdout, args=(process, messages), daemon=True).start()

                def send(value):
                    transcript.write(json.dumps({"direction": "stdin", "frame": value}) + "\n")
                    transcript.flush()
                    process.stdin.write(json.dumps(value) + "\n")
                    process.stdin.flush()

                initialize = str(uuid.uuid4())
                send({"type": "control_request", "request_id": initialize,
                      "request": {"subtype": "initialize", "hooks": None}})
                while True:
                    line = messages.get(timeout=30)
                    if line is None:
                        raise RuntimeError("EOF before initialize")
                    value = json.loads(line)
                    transcript.write(json.dumps({"direction": "stdout", "frame": value}) + "\n")
                    if value.get("type") == "control_response" and value["response"].get("request_id") == initialize:
                        response = value["response"]
                        if response["subtype"] != "success" or response["response"].get("current_permission_mode") != "default":
                            raise RuntimeError("permission handshake failed")
                        break
            prompt = ("Without tools or memory files, reply exactly PROCESS_CONTEXT_48291. Keep this token only in our conversation."
                      if index == 0 else "Without tools, reply only the exact token from my previous message.")
            send({"type": "user", "uuid": str(uuid.uuid4()), "session_id": session,
                  "parent_tool_use_id": None, "message": {"role": "user", "content": [{"type": "text", "text": prompt}]}})
            deadline = time.monotonic() + 120
            while True:
                line = messages.get(timeout=max(0.01, deadline-time.monotonic()))
                if line is None:
                    raise RuntimeError("EOF before result")
                value = json.loads(line)
                transcript.write(json.dumps({"direction": "stdout", "frame": value}) + "\n")
                if value.get("type") == "control_request":
                    send({"type": "control_response", "response": {"subtype": "success", "request_id": value["request_id"],
                          "response": {"behavior": "deny", "message": "This probe allows no tools"}}})
                if value.get("type") == "result":
                    if value.get("subtype") != "success" or value.get("is_error") or value.get("terminal_reason") != "completed" or "PROCESS_CONTEXT_48291" not in value.get("result", ""):
                        raise RuntimeError("context result failed: " + json.dumps(value))
                    break
            elapsed.append(round(time.monotonic()-started, 3))
            if not persistent or index == 1:
                process.stdin.close()
                process.wait(timeout=10)
                if process.returncode != 0:
                    raise RuntimeError("nonzero process exit")
                process = None
        print(json.dumps({"strategy": "persistent" if persistent else "spawn-resume", "session": session,
                          "seconds": elapsed, "wire": str(root / "wire.jsonl"), "context": "passed"}), flush=True)
    finally:
        transcript.close()
        if process is not None:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.wait()


for persistent in (False, True):
    probe(sys.argv[1], persistent)
