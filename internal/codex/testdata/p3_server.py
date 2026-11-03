#!/usr/bin/python3
"""Local P3 acceptance server. No model, credentials, or shell commands."""
import json
import copy
import pathlib
import sys

if "--version" in sys.argv:
    print("P3 local protocol fixture")
    sys.exit()

root = pathlib.Path(sys.argv[sys.argv.index("--fixture-dir") + 1])
root.mkdir(parents=True, exist_ok=True)
path = root / "native-history.json"
threads = json.loads(path.read_text()) if path.exists() else {}
requests = {}


def save():
    temporary = path.with_suffix(".tmp")
    temporary.write_text(json.dumps(threads))
    temporary.replace(path)


def emit(message):
    print(json.dumps(message), flush=True)


def event(thread, method, **params):
    emit({"method": method, "params": {"threadId": thread, **params}})


def metadata(thread):
    data = threads[thread]
    return {
        "id": thread, "cwd": data["cwd"], "preview": data.get("title", ""),
        "parentThreadId": data.get("parentThreadId"),
        "turns": [], "status": {"type": "active" if (root / (thread + ".active")).exists() else "idle"},
    }


for line in sys.stdin:
    m = json.loads(line)
    method, p = m.get("method"), m.get("params", {})
    if method:
        with (root / "methods.jsonl").open("a") as log:
            log.write(json.dumps({"method": method, "threadId": p.get("threadId"),
                                  "cursor": p.get("cursor")}) + "\n")
    result = None
    if method == "initialize":
        result = {"userAgent": "P3-local-fixture"}
    elif method == "thread/start":
        thread = "fixture-" + str(len(threads) + 1)
        threads[thread] = {"cwd": p["cwd"], "turns": []}
        save()
        result = {"thread": metadata(thread), "cwd": p["cwd"], "model": "P3-fixture",
                  "approvalPolicy": "on-request", "sandbox": {"type": "readOnly"}}
    elif method == "thread/resume":
        thread = p["threadId"]
        if (root / (thread + ".occupied")).exists():
            emit({"id": m["id"], "error": {"code": -32600,
                  "message": "thread " + thread + " already has an active writer"}})
            continue
        result = {"thread": metadata(thread), "cwd": threads[thread]["cwd"],
                  "model": "P3-fixture", "approvalPolicy": "on-request",
                  "sandbox": {"type": "readOnly"}}
    elif method == "thread/fork":
        source = p["threadId"]
        assert p["excludeTurns"] and p["deferGoalContinuation"]
        thread = "fixture-fork-" + str(len(threads) + 1)
        threads[thread] = copy.deepcopy(threads[source])
        if p.get("beforeTurnId"):
            boundary = next(index for index, turn in enumerate(threads[thread]["turns"]) if turn["id"] == p["beforeTurnId"])
            threads[thread]["turns"] = threads[thread]["turns"][:boundary]
        save()
        result = {"thread": metadata(thread), "cwd": threads[thread]["cwd"],
                  "model": "P3-fixture", "approvalPolicy": "on-request",
                  "sandbox": {"type": "readOnly"}}
    elif method == "thread/loaded/list":
        result = {"data": list(threads), "nextCursor": None}
    elif method == "thread/list":
        values = [metadata(t) for t in reversed(threads)]
        start = int(p.get("cursor") or "0")
        result = {"data": values[start:start + 20],
                  "nextCursor": str(start + 20) if start + 20 < len(values) else None}
    elif method == "thread/turns/list":
        values = list(reversed(threads[p["threadId"]]["turns"]))
        start = int(p.get("cursor") or "0")
        result = {"data": values[start:start + 20],
                  "nextCursor": str(start + 20) if start + 20 < len(values) else None}
    elif method == "thread/read":
        thread = p["threadId"]
        result = {"thread": {**metadata(thread),
                            "turns": threads[thread]["turns"] if p.get("includeTurns") else []}}
    elif method == "thread/backgroundTerminals/list":
        thread = p["threadId"]
        result = {"data": [{"processId": "background"}] if (root / (thread + ".terminal")).exists() else [],
                  "nextCursor": None}
    elif method == "thread/unsubscribe":
        thread = p["threadId"]
        if (root / (thread + ".unsubscribe-error")).exists():
            emit({"id": m["id"], "error": {"code": -32603, "message": "uncertain unsubscribe"}})
            continue
        result = {"status": "unsubscribed"}
    elif method == "turn/start":
        thread = p["threadId"]
        text = p["input"][0]["text"]
        turn = thread + "-turn-" + str(len(threads[thread]["turns"]) + 1)
        threads[thread]["title"] = threads[thread].get("title", text)
        threads[thread]["turns"].append({
            "id": turn, "status": "inProgress", "itemsView": "full",
            "items": [{"id": turn + "-user", "type": "userMessage",
                       "content": [{"type": "text", "text": text}]}],
        })
        save()
        emit({"id": m["id"], "result": {"turn": {"id": turn, "status": "inProgress"}}})
        event(thread, "turn/started", turn={"id": turn, "status": "inProgress"})
        rid = turn + "-request"
        requests[rid] = (thread, turn)
        emit({"id": rid, "method": "item/commandExecution/requestApproval",
              "params": {"threadId": thread, "turnId": turn, "itemId": turn + "-cmd",
                         "command": "pwd", "cwd": threads[thread]["cwd"],
                         "availableDecisions": ["accept", "decline"]}})
        continue
    elif method == "turn/interrupt":
        thread, turn = p["threadId"], p["turnId"]
        if "--ignore-interrupt" in sys.argv:
            emit({"id": m["id"], "result": {}})
            continue
        threads[thread]["turns"][-1]["status"] = "interrupted"
        save()
        emit({"id": m["id"], "result": {}})
        event(thread, "turn/completed", turn={"id": turn, "status": "interrupted"})
        continue
    elif not method and "id" in m and m["id"] in requests:
        thread, turn = requests.pop(m["id"])
        event(thread, "serverRequest/resolved", requestId=m["id"])
        item = {"id": turn + "-reply", "type": "agentMessage",
                "text": "P3 fixture result in " + threads[thread]["cwd"] +
                        "\n\n" + "\n\n".join("Paragraph " + str(i) + ". " +
                                             "Reading anchor verification. " * 8
                                             for i in range(30))}
        threads[thread]["turns"][-1]["items"].append(item)
        threads[thread]["turns"][-1]["status"] = "completed"
        save()
        event(thread, "item/completed", turnId=turn, item=item)
        event(thread, "turn/completed", turn={"id": turn, "status": "completed"})
        continue
    elif method == "initialized":
        continue
    else:
        if "id" in m:
            emit({"id": m["id"], "error": {"code": -32601, "message": "fixture method unavailable"}})
        continue
    emit({"id": m["id"], "result": result})
