"""sonar tagger plugin template. Standard library only.

After a download sonar asks every enabled tagger for tag values and writes them into the file.
sonar starts this program and talks to it in JSON-RPC 2.0, one JSON object per line:
requests and notes arrive on stdin, answers and notes go out on stdout. stderr goes to
sonar's log. Never print anything else to stdout.

What to replace: tag(). It returns a canned genre.
"""
import json
import sys
import threading

settings = {}  # the plugin's settings from plugin.json, as the user set them
out_lock = threading.Lock()
cancels = {}  # request id -> Event, set when sonar sends $/cancel


def log(*a):
    print(*a, file=sys.stderr, flush=True)


def send(msg):
    msg["jsonrpc"] = "2.0"
    with out_lock:
        sys.stdout.write(json.dumps(msg, ensure_ascii=False) + "\n")
        sys.stdout.flush()


def apply(params):
    """initialize and every $/settings note carry the same full InitParams: re-read all of it."""
    global settings
    settings = params.get("settings") or {}


class RpcError(Exception):
    # -32000 ordinary track error, -32001 site down (sonar tries another downloader),
    # -32002 the user must act (data: {"message", "action"}, action names a "button" setting)
    def __init__(self, code, message, data=None):
        super().__init__(message)
        self.code, self.data = code, data




def tag(req_id, params, cancel):
    """tag {track, ids}: return {"fields": {name: value}}. REPLACE: look the track up.

    params["track"] has title, artist, album, isrc, ...; params["ids"] maps a service name to the
    track's ID there (plugin.json tagger.needs asks sonar to find one first). Known names are
    title, artist, album_artist, album, track, disc, date, isrc, genre, lyrics and cover (an image
    URL). Only fill what you have; a missing or empty value changes nothing in the file.
    """
    return {"fields": {"genre": "Example Genre"}}


METHODS = {"tag": tag}


def serve(msg):
    req_id = msg["id"]
    cancel = cancels[req_id]
    try:
        result = METHODS[msg["method"]](req_id, msg.get("params") or {}, cancel)
        send({"id": req_id, "result": result})
    except RpcError as e:
        err = {"code": e.code, "message": str(e)}
        if e.data is not None:
            err["data"] = e.data
        send({"id": req_id, "error": err})
    except Exception as e:  # any other failure is an ordinary track error
        send({"id": req_id, "error": {"code": -32000, "message": "%s: %s" % (type(e).__name__, e)}})
    finally:
        cancels.pop(req_id, None)


def main():
    sys.stdin.reconfigure(encoding="utf-8")  # sonar speaks UTF-8; Windows defaults to a code page
    sys.stdout.reconfigure(encoding="utf-8")
    for line in sys.stdin:  # ends when sonar closes stdin: exit then
        line = line.strip()
        if not line:
            continue
        msg = json.loads(line)
        method = msg.get("method")
        if "id" not in msg:  # a note: no answer
            if method == "$/settings":
                apply(msg.get("params") or {})
            elif method == "$/cancel":
                ev = cancels.get((msg.get("params") or {}).get("id"))
                if ev:
                    ev.set()
        elif method == "initialize":
            apply(msg.get("params") or {})
            send({"id": msg["id"], "result": {}})
        elif method in METHODS:
            cancels[msg["id"]] = threading.Event()
            threading.Thread(target=serve, args=(msg,), daemon=True).start()
        else:
            send({"id": msg["id"], "error": {"code": -32601, "message": "method not found: %s" % method}})


if __name__ == "__main__":
    main()
