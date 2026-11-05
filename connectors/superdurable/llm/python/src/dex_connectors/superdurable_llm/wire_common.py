# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Strict provider JSON and server-sent event readers."""

import json
from collections.abc import AsyncIterator


def object_value(value: object) -> dict[str, object]:
    """Require a JSON object without coercion."""
    if not isinstance(value, dict):
        raise ValueError("provider response object is invalid")
    return value


def parse_object(body: bytes) -> dict[str, object]:
    """Decode a provider JSON object."""
    try:
        return object_value(json.loads(body))
    except RecursionError as error:
        raise ValueError("provider response JSON is too deeply nested") from error


def string_value(value: object) -> str:
    """Return an optional provider string, rejecting wrong types."""
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("provider response string is invalid")
    return value


def token_count(value: object) -> int:
    """Read a nonnegative token count without numeric coercion."""
    if value is None:
        return 0
    if isinstance(value, bool) or not isinstance(value, int) or value < 0 or value >= 2**63:
        raise ValueError("provider token count is invalid")
    return value


def json_body(body: dict[str, object]) -> bytes:
    """Encode request JSON without whitespace."""
    return json.dumps(body, separators=(",", ":"), ensure_ascii=False, allow_nan=False).encode()


async def sse_events(chunks: AsyncIterator[bytes]) -> AsyncIterator[tuple[str, bytes]]:
    """Yield named SSE events from arbitrary chunk boundaries."""
    buffer = b""
    data: list[bytes] = []
    event_type = ""
    async for chunk in chunks:
        buffer += chunk
        while b"\n" in buffer:
            line, buffer = buffer.split(b"\n", 1)
            line = line.removesuffix(b"\r")
            if not line:
                if data:
                    yield event_type, b"\n".join(data)
                data, event_type = [], ""
            elif line.startswith(b"data:"):
                value = line[5:]
                data.append(value[1:] if value.startswith(b" ") else value)
            elif line.startswith(b"event:"):
                event_type = line[6:].strip().decode("utf-8", errors="replace")
    if buffer or data:
        raise EOFError("provider stream ended inside an event")
