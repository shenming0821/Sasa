import json
from pathlib import Path
import sys


Path("startup-args.json").write_text(json.dumps(sys.argv[1:]))
for line in sys.stdin:
    message = json.loads(line)
    if message["type"] == "control_request":
        response = json.loads(Path("initialize-response.json").read_text())
        print(json.dumps({"type": "control_response", "response": {
            "subtype": "success", "request_id": message["request_id"],
            "response": response}}), flush=True)
    elif message["type"] == "user":
        Path("user-received").touch()
