"""sonar source plugin template. Standard library only.

A source recognises pasted links, shows a track/album/playlist/artist page and searches.
sonar starts this program and talks to it in JSON-RPC 2.0, one JSON object per line:
requests and notes arrive on stdin, answers and notes go out on stdout. stderr goes to
sonar's log. Never print anything else to stdout.

What to replace: the three handlers below return canned data. Make them ask your site.
Which methods sonar may call is listed in plugin.json under source.can.
"""
import json
import sys
import threading

settings = {}  # the plugin's settings from plugin.json, as the user set them
out_lock = threading.Lock()
cancels = {}  # request id -> Event, set when sonar sends $/cancel


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


TRACK = {  # one track page; "service" must equal source.service in plugin.json
    "service": "example",
    "id": "1",
    "name": "Example Song",
    "artists": "Example Artist",
    "album_name": "Example Album",
    "album_id": "10",
    "duration_ms": 180000,
    "images": "",  # a cover URL
    "release_date": "2024-01-01",
    "track_number": 1,
    "external_urls": "https://example.com/track/1",
}


def metadata(req_id, params, cancel):
    """source.metadata {url}: the page of a pasted link. REPLACE: fetch params["url"].

    Shapes: {"service", "track": {...}} for a track; {"service", "album_info", "track_list"} for an
    album; {"service", "playlist_info", "track_list"}; {"service", "artist_info", "album_list",
    "track_list", "top_tracks"}. See docs/plugins.md.
    """
    return {"service": "example", "track": TRACK}


def search(req_id, params, cancel):
    """source.search {query, limit}: REPLACE with a real search of params["query"]."""
    hit = {"id": "1", "name": TRACK["name"], "type": "track", "artists": TRACK["artists"],
           "images": "", "external_urls": TRACK["external_urls"]}
    return {"tracks": [hit], "albums": [], "artists": [], "playlists": []}


def parse_link(req_id, params, cancel):
    """source.parseLink {url}: kind is track, album, playlist or artist. REPLACE: parse params["url"]."""
    return {"kind": "track", "id": "1"}


METHODS = {
    "source.metadata": metadata,
    "source.search": search,
    "source.parseLink": parse_link,
}


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
