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
    root = Path(tempfile.mkdtemp(prefix="sasa-claude-control-" + mode + "-"))
    session = str(uuid.uuid4())
    args = [executable, "-p", "--verbose", "--input-format", "stream-json",
            "--output-format", "stream-json", "--include-partial-messages",
            "--permission-prompt-tool", "stdio", "--permission-mode", "default",
            "--model", "default", "--session-id", session]
    child = subprocess.Popen(args, cwd=root, stdin=subprocess.PIPE,
                             stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                             text=True, start_new_session=True)
    inbox = queue.Queue(maxsize=4096)
    wire = open(root / "wire.jsonl", "w")
    os.chmod(root / "wire.jsonl", 0o600)

    def read():
        for line in child.stdout:
            inbox.put(line)
        inbox.put(None)

    threading.Thread(target=read, daemon=True).start()

    def send(value):
        wire.write(json.dumps({"direction": "stdin", "frame": value}) + "\n")
        wire.flush()
        child.stdin.write(json.dumps(value) + "\n")
        child.stdin.flush()

    prompt = "Use Bash once to write approved.txt containing exactly SASA_CONTROL_OK followed by a newline in this directory. Do not use other tools or write memory files. If denied, do not retry and reply with the denial feedback."
    if mode == "question":
        prompt = "Use AskUserQuestion once to ask which platforms to target, with options Mac and Linux and multiSelect=true. Then reply only with the selected platforms. Do not use other tools or write files or memory."
    deadline = time.monotonic() + 90
    result = None
    initialized = False
    interaction = False
    interrupt_acknowledged = False
    cancellations = 0
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
            if frame.get("type") == "control_response":
                reply = frame["response"]
                if reply.get("request_id") == "initialize-probe" and not initialized:
                    if reply.get("subtype") != "success" or reply.get("response", {}).get("current_permission_mode") != "default":
                        raise RuntimeError("permission initialization failed")
                    initialized = True
                    send({"type": "user", "uuid": str(uuid.uuid4()), "session_id": session,
                          "parent_tool_use_id": None, "message": {"role": "user", "content": [{"type": "text", "text": prompt}]}})
                if reply.get("request_id") == "interrupt-probe" and reply.get("subtype") == "success":
                    interrupt_acknowledged = True
            if frame.get("type") == "control_cancel_request":
                cancellations += 1
            if frame.get("type") == "control_request":
                request = frame["request"]
                if request.get("subtype") != "can_use_tool":
                    raise RuntimeError("unexpected control request")
                expected = "AskUserQuestion" if mode == "question" else "Bash"
                if request.get("tool_name") != expected:
                    raise RuntimeError("unexpected tool: " + request.get("tool_name", ""))
                interaction = True
                if mode == "interrupt":
                    send({"type": "control_request", "request_id": "interrupt-probe", "request": {"subtype": "interrupt"}})
                    continue
                decision = {"behavior": "deny", "message": "SASA_CONTROL_DENIED"}
                if mode == "allow":
                    decision = {"behavior": "allow", "updatedInput": request["input"]}
                elif mode == "question":
                    updated = dict(request["input"])
                    updated["answers"] = {question["question"]: "Mac, Linux" for question in updated["questions"]}
                    decision = {"behavior": "allow", "updatedInput": updated}
                send({"type": "control_response", "response": {"subtype": "success", "request_id": frame["request_id"], "response": decision}})
            if frame.get("type") == "result":
                result = {key: frame.get(key) for key in ("subtype", "is_error", "terminal_reason")}
                child.stdin.close()
        child.wait(timeout=10)
        proof = (root / "approved.txt").read_text() if (root / "approved.txt").exists() else None
        if not interaction or result is None:
            raise RuntimeError("incomplete control recording")
        if mode != "interrupt" and (child.returncode != 0 or result != {"subtype": "success", "is_error": False, "terminal_reason": "completed"}):
            raise RuntimeError("unsuccessful control result")
        if mode == "allow" and proof != "SASA_CONTROL_OK\n":
            raise RuntimeError("allowed file mismatch")
        if mode != "allow" and proof is not None:
            raise RuntimeError("denied or interrupted write occurred")
        if mode == "interrupt" and not interrupt_acknowledged:
            raise RuntimeError("interrupt receipt missing")
        if mode == "interrupt" and result["terminal_reason"] not in ("interrupted", "aborted_tools", "aborted_streaming"):
            raise RuntimeError("unconfirmed interruption result")
        print(json.dumps({"mode": mode, "session": session, "exit": child.returncode,
                          "result": result, "interrupt_acknowledged": interrupt_acknowledged,
                          "cancellations": cancellations, "proof": proof,
                          "wire": str(root / "wire.jsonl")}), flush=True)
    finally:
        wire.close()
        try:
            os.killpg(child.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        child.wait()


for mode in (sys.argv[2:] or ("allow", "deny", "question", "interrupt")):
    probe(sys.argv[1], mode)
