import json
from pathlib import Path
import sys


mode = sys.argv[1]
records = [json.loads(line) for line in Path(__file__).with_name("mac-control-" + mode + ".jsonl").read_text().splitlines()]
initialize = next(record["frame"]["response"]["response"] for record in records
                  if record["direction"] == "stdout" and record["frame"].get("type") == "control_response"
                  and record["frame"]["response"]["request_id"] == "initialize-probe")
permission = next(record["frame"] for record in records
                  if record["direction"] == "stdout" and record["frame"]["type"] == "control_request")
terminal = next(record["frame"] for record in records if record["frame"]["type"] == "result")
expected = next((record["frame"]["response"] for record in records
                 if record["direction"] == "stdin" and record["frame"]["type"] == "control_response"), None)


def emit(frame):
    print(json.dumps(frame), flush=True)


for line in sys.stdin:
    message = json.loads(line)
    if message["type"] == "control_request" and message["request"]["subtype"] == "initialize":
        emit({"type": "control_response", "response": {"subtype": "success",
              "request_id": message["request_id"], "response": initialize}})
    elif message["type"] == "user":
        terminal["session_id"] = message["session_id"]
        emit(permission)
    elif message["type"] == "control_request" and message["request"]["subtype"] == "interrupt":
        if mode != "interrupt":
            raise RuntimeError("unexpected interrupt")
        cancellation = next(record["frame"] for record in records if record["frame"]["type"] == "control_cancel_request")
        emit(cancellation)
        emit({"type": "control_response", "response": {"subtype": "success", "request_id": message["request_id"], "response": {}}})
        emit(terminal)
        sys.exit(1)
    elif message["type"] == "control_response":
        if message["response"] != expected:
            raise RuntimeError("adapter response differs from recorded native response")
        Path("matched-response.txt").write_text(mode)
        emit(terminal)
