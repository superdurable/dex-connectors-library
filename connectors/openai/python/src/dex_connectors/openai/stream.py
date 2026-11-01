# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Responses API server-sent event reading for createResponse."""

import json
from collections.abc import Awaitable, Callable
from dataclasses import dataclass

import httpx
from dex_connectors.sdk import Call, Progress

from .models import Response
from .wire import decode_wire_response, is_safe_response_id

_TERMINAL_EVENT_TYPES = ("response.completed", "response.failed", "response.incomplete")
_PROGRESS_EVENT_TYPES = ("response.created", "response.queued", "response.in_progress")


@dataclass(frozen=True, slots=True)
class StreamOutcome:
    """Terminal response, invalid ID, or the reason the stream ended without one."""

    terminal_event_type: str = ""
    terminal_response: Response | None = None
    has_invalid_response_id: bool = False
    error_message: str = ""


class _StreamLimitError(Exception):
    pass


class ResponseStreamReader:
    """Read one createResponse event stream, reporting progress and text best effort."""

    def __init__(
        self,
        call: Call,
        response: httpx.Response,
        max_total_bytes: int,
        max_event_bytes: int,
        record_response_id: Callable[[str], Awaitable[bool]],
    ) -> None:
        self._call = call
        self._response = response
        self._max_total_bytes = max_total_bytes
        self._max_event_bytes = max_event_bytes
        self._record_response_id = record_response_id
        self.response_id = ""
        self.has_stream_error = False
        self._is_delivery_stopped = False

    async def read(self) -> StreamOutcome:
        """Read until a terminal event for the observed response, or explain why none arrived."""
        chunks = self._response.aiter_bytes()
        try:
            return await self._read_events(chunks)
        except _StreamLimitError as error:
            if self.response_id:
                await self._drain(chunks)
            return StreamOutcome(error_message=str(error))
        except httpx.HTTPError:
            return StreamOutcome(error_message="stream could not be read")

    async def _read_events(self, chunks: object) -> StreamOutcome:
        pending = b""
        data_lines: list[bytes] = []
        data_length = 0
        total_bytes = 0
        async for chunk in chunks:  # type: ignore[attr-defined]
            pending += chunk
            while True:
                newline = pending.find(b"\n")
                if newline < 0:
                    break
                line = pending[: newline + 1]
                pending = pending[newline + 1 :]
                total_bytes += len(line)
                outcome = await self._consume_line(line, total_bytes, data_lines, data_length)
                if isinstance(outcome, StreamOutcome):
                    return outcome
                data_length = outcome
            self._check_limits(total_bytes + len(pending), len(pending), data_length)
        if pending:
            total_bytes += len(pending)
            outcome = await self._consume_line(pending, total_bytes, data_lines, data_length)
            if isinstance(outcome, StreamOutcome):
                return outcome
        return StreamOutcome(error_message="stream ended before a terminal event")

    async def _consume_line(
        self, line: bytes, total_bytes: int, data_lines: list[bytes], data_length: int
    ) -> StreamOutcome | int:
        self._check_limits(total_bytes, len(line), data_length)
        trimmed = line.removesuffix(b"\n").removesuffix(b"\r")
        if trimmed.startswith(b"data:"):
            value = trimmed[len(b"data:") :].strip()
            data_length += len(value) + (1 if data_lines else 0)
            data_lines.append(value)
        if not trimmed and data_lines:
            data = b"\n".join(data_lines)
            data_lines.clear()
            outcome = await self._handle_event(data)
            if outcome is not None:
                return outcome
            return 0
        return data_length

    def _check_limits(self, total_bytes: int, line_bytes: int, data_length: int) -> None:
        if total_bytes > self._max_total_bytes:
            raise _StreamLimitError("stream exceeds the configured size limit")
        if line_bytes + data_length > self._max_event_bytes:
            raise _StreamLimitError("stream event exceeds the configured size limit")

    async def _handle_event(self, data: bytes) -> StreamOutcome | None:
        try:
            event = json.loads(data)
            if not isinstance(event, dict):
                raise ValueError
            event_type = event.get("type") or ""
            delta = event.get("delta") or ""
            sequence = event.get("sequence_number")
            if not isinstance(event_type, str) or not isinstance(delta, str):
                raise ValueError
            if sequence is not None and type(sequence) is not int:
                raise ValueError
            response = decode_wire_response(event.get("response") or {})
        except (ValueError, TypeError, UnicodeDecodeError):
            return None
        if response.id and not self.response_id:
            if not is_safe_response_id(response.id):
                return StreamOutcome(has_invalid_response_id=True)
            self.response_id = response.id
            await self._record_response_id(response.id)
        if event_type in _PROGRESS_EVENT_TYPES:
            self._deliver(
                lambda: self._call.report_progress(
                    Progress(event_type, response.status, provider_sequence=sequence)
                )
            )
        elif event_type == "response.output_text.delta":
            self._deliver(lambda: self._call.write_text(delta))
        elif event_type in _TERMINAL_EVENT_TYPES:
            if response.id and response.id == self.response_id:
                return StreamOutcome(terminal_event_type=event_type, terminal_response=response)
        elif event_type == "error":
            self.has_stream_error = True
        return None

    def _deliver(self, write: Callable[[], None]) -> None:
        if self._is_delivery_stopped:
            return
        try:
            write()
        except Exception:  # noqa: BLE001 - Stream delivery is best effort and never decides the outcome.
            self._is_delivery_stopped = True

    async def _drain(self, chunks: object) -> None:
        try:
            async for _ in chunks:  # type: ignore[attr-defined]
                pass
        except httpx.HTTPError:
            return  # A failed drain leaves the outcome to the read-back.
