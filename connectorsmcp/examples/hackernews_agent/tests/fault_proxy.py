# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""An MCP reverse proxy that records tool calls and can lose one answer.

The proxy forwards every request to the server. For a tool named in
``tool_names_to_lose_first_answer``, it forwards the first ``tools/call``,
lets the server finish it, and answers the agent ``502`` instead. The tool
ran but the agent never saw the result, as when a gateway drops a response.
"""

import json
import threading
import urllib.error
import urllib.request
from collections.abc import Iterable
from dataclasses import dataclass
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

IDEMPOTENCY_KEY_META_KEY = "io.superdurable.dex/idempotencyKey"
REPLAYED_META_KEY = "io.superdurable.connectors/replayed"
UPSTREAM_TIMEOUT_SECONDS = 120
# Hop-by-hop and framing headers the proxy sets itself.
UNFORWARDED_HEADERS = {
    "host",
    "content-length",
    "connection",
    "transfer-encoding",
    "keep-alive",
    "accept-encoding",
}


@dataclass(frozen=True)
class RecordedToolCall:
    """One ``tools/call`` that passed the proxy.

    Attributes:
        tool_name: The MCP tool name.
        idempotency_key: The request's ``io.superdurable.dex/idempotencyKey``.
        upstream_status: The server's HTTP status.
        upstream_result: The server's JSON-RPC ``result``, or ``None``.
        was_answer_lost: Whether the proxy answered ``502`` instead of the result.
    """

    tool_name: str
    idempotency_key: str | None
    upstream_status: int
    upstream_result: dict[str, Any] | None
    was_answer_lost: bool

    @property
    def is_replayed(self) -> bool:
        """Whether the server answered from a stored result."""
        meta = (self.upstream_result or {}).get("_meta") or {}
        return meta.get(REPLAYED_META_KEY) is True


class ToolCallFaultProxy:
    """Serve the proxy on a free loopback port in a background thread."""

    def __init__(self, upstream_url: str, tool_names_to_lose_first_answer: Iterable[str] = ()) -> None:
        """Start the proxy.

        Args:
            upstream_url: The server's MCP endpoint.
            tool_names_to_lose_first_answer: MCP tool names whose first answer is lost.
        """
        self._upstream_url = upstream_url
        self._tool_names_to_lose = set(tool_names_to_lose_first_answer)
        self._lock = threading.Lock()
        self._recorded_calls: list[RecordedToolCall] = []
        self._http_server = ThreadingHTTPServer(("127.0.0.1", 0), self._handler_class())
        self._thread = threading.Thread(target=self._http_server.serve_forever, daemon=True)
        self._thread.start()

    @property
    def url(self) -> str:
        """The proxy's MCP endpoint."""
        return f"http://127.0.0.1:{self._http_server.server_address[1]}/mcp"

    def recorded_calls(self, tool_name: str) -> list[RecordedToolCall]:
        """Return the calls of one MCP tool, in arrival order."""
        with self._lock:
            return [call for call in self._recorded_calls if call.tool_name == tool_name]

    def close(self) -> None:
        """Stop serving and join the thread."""
        self._http_server.shutdown()
        self._http_server.server_close()
        self._thread.join()

    def _handler_class(self) -> type[BaseHTTPRequestHandler]:
        proxy = self

        class ForwardingHandler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def do_POST(self) -> None:  # noqa: N802
                proxy._forward(self)

            def do_GET(self) -> None:  # noqa: N802
                proxy._forward(self)

            def do_DELETE(self) -> None:  # noqa: N802
                proxy._forward(self)

            def log_message(self, format: str, *args: Any) -> None:  # noqa: A002
                return

        return ForwardingHandler

    def _forward(self, handler: BaseHTTPRequestHandler) -> None:
        body = handler.rfile.read(int(handler.headers.get("Content-Length") or 0))
        headers = {
            name: value for name, value in handler.headers.items() if name.lower() not in UNFORWARDED_HEADERS
        }
        request = urllib.request.Request(
            self._upstream_url, data=body or None, headers=headers, method=handler.command
        )
        try:
            with urllib.request.urlopen(request, timeout=UPSTREAM_TIMEOUT_SECONDS) as response:
                status, response_headers, response_body = response.status, response.headers, response.read()
        except urllib.error.HTTPError as error:
            status, response_headers, response_body = error.code, error.headers, error.read()
        is_answer_lost = self._record_tool_call(
            body, status, response_headers.get("Content-Type", ""), response_body
        )
        if is_answer_lost:
            status, response_body = 502, b"the proxy lost this answer on purpose"
            response_headers = {"Content-Type": "text/plain"}
        handler.send_response(status)
        for name, value in response_headers.items():
            if name.lower() not in UNFORWARDED_HEADERS:
                handler.send_header(name, value)
        handler.send_header("Content-Length", str(len(response_body)))
        handler.send_header("Connection", "close")
        handler.end_headers()
        handler.wfile.write(response_body)
        handler.close_connection = True

    def _record_tool_call(self, body: bytes, status: int, content_type: str, response_body: bytes) -> bool:
        """Record a ``tools/call`` and return whether its answer is lost."""
        try:
            message = json.loads(body) if body else None
        except json.JSONDecodeError:
            return False
        if not isinstance(message, dict) or message.get("method") != "tools/call":
            return False
        params = message.get("params") or {}
        tool_name = str(params.get("name"))
        with self._lock:
            is_answer_lost = tool_name in self._tool_names_to_lose
            self._tool_names_to_lose.discard(tool_name)
            self._recorded_calls.append(
                RecordedToolCall(
                    tool_name=tool_name,
                    idempotency_key=(params.get("_meta") or {}).get(IDEMPOTENCY_KEY_META_KEY),
                    upstream_status=status,
                    upstream_result=_json_rpc_result(content_type, response_body),
                    was_answer_lost=is_answer_lost,
                )
            )
        return is_answer_lost


def _json_rpc_result(content_type: str, response_body: bytes) -> dict[str, Any] | None:
    """Decode the JSON-RPC result of a JSON or Server-Sent Events answer."""
    text = response_body.decode("utf-8", errors="replace")
    if content_type.startswith("text/event-stream"):
        payloads = [line[len("data:") :].strip() for line in text.splitlines() if line.startswith("data:")]
    else:
        payloads = [text]
    for payload in reversed(payloads):
        try:
            message = json.loads(payload)
        except json.JSONDecodeError:
            continue
        if isinstance(message, dict) and isinstance(message.get("result"), dict):
            return message["result"]
    return None
