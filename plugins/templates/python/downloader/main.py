"""sonar downloader plugin template. Standard library only.

sonar starts this program and talks to it in JSON-RPC 2.0, one JSON object per line:
requests and notes arrive on stdin, answers and notes go out on stdout. Whatever you
print to stderr lands in sonar's log. Never print anything else to stdout.

What to replace: download(). Everything else is the protocol and can stay.
"""
import json
import os
import sys
import threading
import urllib.error
import urllib.parse
import urllib.request

CHUNK = 256 * 1024  # a $/progress note per chunk; a download silent for 60 s counts as hung

settings = {}        # the plugin's settings from plugin.json, as the user set them
proxy = ""           # effectiveProxy: the proxy sonar itself would use, "" for direct
out_lock = threading.Lock()
cancels = {}         # request id -> Event, set when sonar sends $/cancel


def log(*a):
    print(*a, file=sys.stderr, flush=True)


def send(msg):
    msg["jsonrpc"] = "2.0"
    with out_lock:
        sys.stdout.write(json.dumps(msg, ensure_ascii=False) + "\n")
        sys.stdout.flush()


def apply(params):
    """initialize and every $/settings note carry the same full InitParams: re-read all of it."""
    global settings, proxy
    settings = params.get("settings") or {}
    proxy = params.get("effectiveProxy") or ""


class RpcError(Exception):
    # -32000 ordinary track error, -32001 site down (sonar tries another downloader),
    # -32002 the user must act (data: {"message", "action"}, action names a "button" setting)
    def __init__(self, code, message, data=None):
        super().__init__(message)
        self.code, self.data = code, data


def opener(url):
    host = urllib.parse.urlparse(url).hostname or ""
    if proxy and host not in ("localhost", "127.0.0.1", "::1"):
        # urllib speaks http(s) proxies only; a socks5:// proxy needs a library
        return urllib.request.build_opener(urllib.request.ProxyHandler({"http": proxy, "https": proxy}))
    return urllib.request.build_opener(urllib.request.ProxyHandler({}))


def download(req_id, params, cancel):
    """Save the track next to params["pathNoExt"] and return the full path of the file.

    params["track"] has title, artist, album, isrc, duration_ms, ...; params["ids"] maps a
    service name to the track's ID there ("amazon": "B0..."), for downloaders that need one.
    """
    track = params["track"]
    name = "%s - %s.flac" % (track["artist"], track["title"])
    url = settings.get("baseUrl", "").rstrip("/") + "/" + urllib.parse.quote(name)
    path = params["pathNoExt"] + ".flac"  # the extension is yours: sonar reads the format from it
    os.makedirs(os.path.dirname(path), exist_ok=True)
    try:
        with opener(url).open(url, timeout=30) as resp, open(path + ".part", "wb") as f:
            size = int(resp.headers.get("Content-Length") or 0)
            done = 0
            while True:
                if cancel.is_set():
                    raise RpcError(-32000, "cancelled")
                chunk = resp.read(CHUNK)
                if not chunk:
                    break
                f.write(chunk)
                done += len(chunk)
                send({"method": "$/progress", "params": {"id": req_id, "bytes": done, "size": size, "status": ""}})
    except urllib.error.HTTPError as e:
        cleanup(path)
        raise RpcError(-32001 if e.code >= 500 else -32000, "%s: HTTP %d" % (url, e.code))
    except (urllib.error.URLError, OSError) as e:
        cleanup(path)
        raise RpcError(-32001, "%s: %s" % (url, e))
    except RpcError:
        cleanup(path)
        raise
    os.replace(path + ".part", path)
    return {"path": path}


def cleanup(path):
    try:
        os.remove(path + ".part")
    except OSError:
        pass


METHODS = {"download": download}


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
