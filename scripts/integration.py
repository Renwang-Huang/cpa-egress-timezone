#!/usr/bin/env python3
"""Isolated CPA integration: fake credentials, local upstream and two exit proxies.

Requires Docker and a built library. No production config or real model API is used.
All probe/GeoIP replies here are simulated; this is not a public egress-IP test.
"""
import argparse
import base64
import datetime as dt
import hashlib
import http.server
import json
import os
from pathlib import Path
import select
import shutil
import socket
import struct
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from zoneinfo import ZoneInfo

ROOT = Path(__file__).resolve().parents[1]
KEY = "isolated-test-client"
MANAGEMENT = "isolated-test-management"
CAPTURES = []
OBSERVATIONS = []
EXITS = {"a": "8.8.8.8", "b": "1.1.1.1"}
ZONES = {"8.8.8.8": "America/Los_Angeles", "1.1.1.1": "Asia/Tokyo"}
FAIL_PROBE = set()
# These are synthetic fixture identities, not a user's client fingerprint.
USER_AGENT = "cpa-timezone-integration/1.0"
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def exact(stream, n):
    out = bytearray()
    while len(out) < n:
        part = stream.read(n - len(out))
        if not part:
            raise EOFError("closed WebSocket")
        out.extend(part)
    return bytes(out)


def read_frame(stream):
    first, second = exact(stream, 2)
    size = second & 127
    if size == 126:
        size = struct.unpack("!H", exact(stream, 2))[0]
    elif size == 127:
        size = struct.unpack("!Q", exact(stream, 8))[0]
    if size > 4 * 1024 * 1024:
        raise ValueError("oversized test frame")
    mask = exact(stream, 4) if second & 128 else b""
    data = exact(stream, size)
    if mask:
        data = bytes(b ^ mask[i % 4] for i, b in enumerate(data))
    return first & 15, data


def frame(data, masked=False, opcode=1):
    flag = 128 if masked else 0
    if len(data) < 126:
        head = bytes([128 | opcode, flag | len(data)])
    elif len(data) < 65536:
        head = bytes([128 | opcode, flag | 126]) + struct.pack("!H", len(data))
    else:
        head = bytes([128 | opcode, flag | 127]) + struct.pack("!Q", len(data))
    if not masked:
        return head + data
    mask = os.urandom(4)
    return head + mask + bytes(b ^ mask[i % 4] for i, b in enumerate(data))


def events():
    response = {"id": "resp_test", "object": "response", "model": "gpt-5.1-codex",
                "status": "completed", "output": [{"id": "msg_test", "type": "message",
                "role": "assistant", "status": "completed", "content": [{"type": "output_text",
                "text": "ok", "annotations": []}]}],
                "usage": {"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}
    return [{"type": "response.created", "response": dict(response, status="in_progress", output=[])},
            {"type": "response.completed", "response": response}]


class Mock(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_):
        pass

    def reply(self, body, code=200, content_type="application/json"):
        self.send_response(code)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_CONNECT(self):
        # websocket.Dialer uses CONNECT even for ws:// through an HTTP proxy.
        host, port = self.path.rsplit(":", 1)
        if host != "127.0.0.1" or int(port) != self.server.upstream_port:
            self.reply(b"{}", 403)
            return
        with socket.create_connection((host, int(port)), timeout=10) as upstream:
            self.send_response(200)
            self.end_headers()
            self.wfile.flush()
            self.close_connection = True
            sockets = [self.connection, upstream]
            while True:
                readable, _, _ = select.select(sockets, [], [], 15)
                if not readable:
                    return
                for source in readable:
                    data = source.recv(65536)
                    if not data:
                        return
                    (upstream if source is self.connection else self.connection).sendall(data)

    def do_GET(self):
        path = urllib.parse.urlsplit(self.path).path
        route = self.server.route
        if path == "/cdn-cgi/trace":
            OBSERVATIONS.append((route, "trace", dict(self.headers)))
            if route in FAIL_PROBE:
                self.reply(b"unavailable", 503)
            else:
                self.reply(("ip=" + EXITS[route] + "\n").encode(), content_type="text/plain")
            return
        if path.startswith("/geo/"):
            ip = urllib.parse.unquote(path[5:])
            OBSERVATIONS.append((route, "geo", dict(self.headers)))
            self.reply(json.dumps({"ip": ip, "success": True, "timezone": {"id": ZONES[ip]}}).encode())
            return
        if self.headers.get("Upgrade", "").lower() != "websocket":
            self.reply(b"{}", 404)
            return
        accept = base64.b64encode(hashlib.sha1((self.headers["Sec-WebSocket-Key"] + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()
        self.send_response(101)
        self.send_header("Upgrade", "websocket")
        self.send_header("Connection", "Upgrade")
        self.send_header("Sec-WebSocket-Accept", accept)
        self.end_headers()
        try:
            while True:
                opcode, raw = read_frame(self.rfile)
                if opcode == 8:
                    return
                if opcode == 9:
                    self.wfile.write(frame(raw, opcode=10))
                    self.wfile.flush()
                if opcode == 1:
                    CAPTURES.append(("websocket", json.loads(raw), dict(self.headers), route))
                    for event in events():
                        self.wfile.write(frame(json.dumps(event).encode()))
                    self.wfile.flush()
        except (EOFError, ConnectionError):
            pass

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        CAPTURES.append(("http", body, dict(self.headers), self.server.route))
        payload = "".join("event: " + e["type"] + "\ndata: " + json.dumps(e) + "\n\n" for e in events()).encode()
        self.reply(payload, content_type="text/event-stream")


def request(port, path, body=None, management=False):
    raw = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(f"http://127.0.0.1:{port}" + path, data=raw,
          headers={"Authorization": "Bearer " + (MANAGEMENT if management else KEY),
                   "Content-Type": "application/json", "User-Agent": USER_AGENT,
                   "Originator": "timezone-test"})
    with OPENER.open(req, timeout=20) as response:
        return response.read()


def websocket(port, body):
    with socket.create_connection(("127.0.0.1", port), timeout=20) as sock:
        stream = sock.makefile("rb")
        key = base64.b64encode(os.urandom(16)).decode()
        sock.sendall((f"GET /v1/responses HTTP/1.1\r\nHost: 127.0.0.1:{port}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\nAuthorization: Bearer {KEY}\r\nUser-Agent: {USER_AGENT}\r\nOriginator: timezone-test\r\n\r\n").encode())
        assert b"101" in stream.readline(), "WebSocket handshake failed"
        while stream.readline() != b"\r\n":
            pass
        sock.sendall(frame(json.dumps(dict(body, type="response.create")).encode(), masked=True))
        for _ in range(30):
            opcode, raw = read_frame(stream)
            if opcode == 9:
                sock.sendall(frame(raw, masked=True, opcode=10))
            if opcode == 1:
                event = json.loads(raw)
                assert event.get("type") != "error", event
                if event.get("type") == "response.completed":
                    sock.sendall(frame(b"", masked=True, opcode=8))
                    return
        raise AssertionError("WebSocket did not complete")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", default="eceasy/cli-proxy-api:v8.0.47")
    parser.add_argument("--library", type=Path, default=ROOT / "build/linux/amd64/egress-timezone.so")
    args = parser.parse_args()
    name = "cpa-egress-timezone-test-" + str(os.getpid())
    checks = []
    expected_identity = None
    servers = []
    with tempfile.TemporaryDirectory(prefix="cpa-egress-timezone-") as temp:
        stage = Path(temp)
        for route in ("a", "b", "a"):
            server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Mock)
            server.route = route
            servers.append(server)
            threading.Thread(target=server.serve_forever, daemon=True).start()
        upstream = servers[2].server_address[1]
        for server in servers:
            server.upstream_port = upstream
        with socket.socket() as port_probe:
            port_probe.bind(("127.0.0.1", 0))
            port = port_probe.getsockname()[1]
        (stage / "plugins").mkdir()
        shutil.copyfile(args.library, stage / "plugins/egress-timezone.so")
        config = {"host": "127.0.0.1", "port": port, "auth-dir": "/tmp/empty-timezone-auths",
                  "api-keys": [KEY], "request-retry": 0,
                  "remote-management": {"secret-key": MANAGEMENT, "allow-remote": False, "disable-control-panel": True},
                  "codex-api-key": [{"api-key": "fake-upstream-" + route,
                      "base-url": f"http://127.0.0.1:{upstream}", "websockets": False,
                      "proxy-url": f"http://127.0.0.1:{servers[i].server_address[1]}",
                      "headers": {"User-Agent": USER_AGENT, "Originator": "codex-tui"},
                      "models": [{"name": "gpt-5.1-codex", "alias": "timezone-" + route}]} for i, route in enumerate(("a", "b"))],
                  "plugins": {"enabled": True, "dir": "/timezone/plugins", "configs": {"egress-timezone": {
                      "enabled": True, "priority": 1000, "allow_http": True, "cache_ttl_seconds": 1,
                      "geoip_url": "http://geo.invalid/geo/{ip}", "cpa_config_path": "/timezone/config.yaml"}}}}

        def stop():
            subprocess.run(["docker", "rm", "-f", name], capture_output=True)

        def start(ws, enabled=True, v8=False):
            # Restart only this isolated container; never touch production.
            config["plugins"]["configs"]["egress-timezone"]["enabled"] = enabled
            for entry in config["codex-api-key"]:
                entry["websockets"] = ws
            # JSON is a YAML subset. No Python YAML dependency is needed.
            selected_config = config
            if v8:
                selected_config = dict(config)
                selected_config["config-version"] = 8
                selected_config["server"] = {"host": selected_config.pop("host"), "port": selected_config.pop("port")}
                selected_config["access"] = {"api-keys": selected_config.pop("api-keys")}
                selected_config["oauth"] = {"auth-dir": selected_config.pop("auth-dir")}
                selected_config["management"] = selected_config.pop("remote-management")
                groups = []
                for index, entry in enumerate(selected_config.pop("codex-api-key")):
                    entry = dict(entry)
                    key = {k: entry.pop(k) for k in ("api-key", "websockets")}
                    groups.append(dict(entry, name="test-" + str(index), keys=[key]))
                selected_config["api-keys"] = {"codex": groups}
            (stage / "config.yaml").write_text(json.dumps(selected_config))
            subprocess.run(["docker", "run", "--detach", "--name", name, "--network", "host",
                "--mount", f"type=bind,src={stage},dst=/timezone,readonly", args.image,
                "./CLIProxyAPI", "--config", "/timezone/config.yaml", "--local-model"], check=True, capture_output=True)
            for _ in range(100):
                try:
                    status = json.loads(request(port, "/v8/management/plugins", management=True))
                    plugin = next(p for p in status["plugins"] if p["id"] == "egress-timezone")
                    if bool(plugin.get("effective_enabled")) == enabled:
                        if enabled:
                            assert plugin["metadata"]["version"] == "2.0.0"
                        return
                except (urllib.error.URLError, KeyError, StopIteration):
                    pass
                time.sleep(.1)
            raise RuntimeError("isolated CPA/plugin did not become ready")

        def payload(route="a"):
            source_date = dt.datetime.now(ZoneInfo("Asia/Hong_Kong")).date().isoformat()
            context = f"<environment_context><current_date>{source_date}</current_date><timezone>Asia/Hong_Kong</timezone></environment_context>"
            return {"model": "timezone-" + route, "input": [{"role": "user", "content": [{"type": "input_text", "text": context}]}]}

        def verify(label, zone, transport="http", route=None):
            kind, body, headers, captured_route = CAPTURES[-1]
            serialized = json.dumps(body)
            assert kind == transport, (label, kind)
            assert zone in serialized, (label, body)
            if route:
                assert captured_route == route, (label, captured_route)
            if zone != "Asia/Hong_Kong":
                assert "Asia/Hong_Kong" not in serialized, (label, body)
                expected_date = dt.datetime.now(ZoneInfo(zone)).date().isoformat()
                assert f"<current_date>{expected_date}</current_date>" in serialized, (label, body)
            normalized = {k.lower(): v for k, v in headers.items()}
            if expected_identity is not None:
                assert {k: normalized.get(k) for k in expected_identity} == expected_identity, (label, "identity changed")
            checks.append(label)
            print("PASS", label, flush=True)

        try:
            start(False, enabled=False)
            request(port, "/v1/responses", payload())
            baseline_headers = {k.lower(): v for k, v in CAPTURES[-1][2].items()}
            expected_identity = {k: baseline_headers.get(k) for k in ("user-agent", "originator")}
            verify("baseline with plugin disabled", "Asia/Hong_Kong", route="a")
            stop()
            start(False)
            for stream in (False, True):
                request(port, "/v1/responses", dict(payload(), stream=stream))
                verify(f"Responses stream={stream}", "America/Los_Angeles", route="a")
            request(port, "/v1/responses", payload("b"))
            verify("selected account uses proxy B", "Asia/Tokyo", route="b")
            body = payload()
            request(port, "/v1/chat/completions", {"model": "timezone-a", "messages": [{"role": "user", "content": body["input"][0]["content"][0]["text"]}]})
            verify("Chat Completions", "America/Los_Angeles", route="a")
            geo_count = sum(kind == "geo" for _, kind, _ in OBSERVATIONS)
            time.sleep(1.1)
            request(port, "/v1/responses", payload())
            verify("unchanged exit reuses GeoIP mapping", "America/Los_Angeles", route="a")
            assert sum(kind == "geo" for _, kind, _ in OBSERVATIONS) == geo_count
            EXITS["a"] = "1.1.1.1"
            time.sleep(1.1)
            request(port, "/v1/responses", payload())
            verify("changed exit refreshes timezone", "Asia/Tokyo", route="a")
            FAIL_PROBE.add("a")
            time.sleep(1.1)
            request(port, "/v1/responses", payload())
            verify("failed refresh preserves source timezone", "Asia/Hong_Kong", route="a")
            status = json.loads(request(port, "/v0/management/plugins/egress-timezone/status", management=True))
            assert status["skipped_calls"] > 0 and status["last_skip_reason"], status
            checks.append("management diagnostics")
            for _, _, headers in OBSERVATIONS:
                assert not any(k.lower() in ("authorization", "cookie", "originator") for k in headers), headers
            checks.append("probes carry no provider identity")
            stop()
            EXITS["a"] = "8.8.8.8"
            FAIL_PROBE.clear()
            start(True, v8=True)
            request(port, "/v1/responses", payload("b"))
            verify("v8 grouped config and proxy B", "Asia/Tokyo", route="b")
            websocket(port, payload())
            verify("Responses WebSocket", "America/Los_Angeles", "websocket")
            result = {"passed": len(checks), "checks": checks, "image": args.image,
                      "library_sha256": hashlib.sha256(args.library.read_bytes()).hexdigest(),
                      "verified_at": dt.datetime.now(dt.timezone.utc).isoformat(),
                      "scope": "isolated CPA, synthetic exits and GeoIP; zero real model requests"}
            output = ROOT / "build/integration-results.json"
            output.parent.mkdir(exist_ok=True)
            output.write_text(json.dumps(result, indent=2) + "\n")
            print(json.dumps(result, ensure_ascii=False, indent=2))
        except Exception:
            log = subprocess.run(["docker", "logs", "--tail", "35", name], capture_output=True, text=True)
            print(log.stdout + log.stderr)
            raise
        finally:
            stop()
            for server in servers:
                server.shutdown()
                server.server_close()


if __name__ == "__main__":
    main()
