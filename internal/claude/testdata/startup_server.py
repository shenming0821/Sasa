import json
from pathlib import Path
import sys


for line in sys.stdin:
    message = json.loads(line)
    if message["type"] == "control_request":
        marker = Path("initialize-ready")
        if not marker.exists():
            marker.touch()
            continue
        print(json.dumps({"type": "control_response", "response": {
            "subtype": "success", "request_id": message["request_id"],
            "response": {"current_permission_mode": "default"}}}), flush=True)
    elif message["type"] == "user":
        Path("user-received").write_text(message["uuid"])
        print(json.dumps({"type": "result", "subtype": "success", "is_error": False,
                          "terminal_reason": "completed", "session_id": message["session_id"]}), flush=True)
