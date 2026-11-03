# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Credential-safe HTTP provider fixture for text-generation connector tests.

The fixture uses ``httpx.MockTransport``. It exercises the real Query HTTP
exchange without opening a socket; Dex Worker and Client behavior still needs
the consuming connector's integration test.
"""

from __future__ import annotations

import asyncio
from collections.abc import AsyncIterator, Callable
from dataclasses import dataclass, field

import httpx

from ..types import Call
from .contracts import CredentialHeader, TextGenerationRequest
from .query import TextGenerationQuery

_MAX_RECORDED_REQUEST_BYTES = 16 << 20
_REDACTED = b"[REDACTED]"


@dataclass(frozen=True, slots=True)
class FakeStreamChunk:
    """One ``data`` body chunk sent after ``delay_seconds`` of silence."""

    data: bytes
    delay_seconds: float = 0


@dataclass(frozen=True, slots=True)
class FakeReply:
    """One queued response; zero values send an empty HTTP 200 body.

    ``status_code``, ``headers``, and ``body`` form the response.
    ``delay_seconds`` delays headers; ``stream_chunks`` adds delayed body
    pieces. ``should_drop_connection`` simulates a pre-response transport error.
    """

    status_code: int = 200
    headers: dict[str, str] = field(default_factory=dict)
    body: bytes = b""
    delay_seconds: float = 0
    stream_chunks: tuple[FakeStreamChunk, ...] = ()
    should_drop_connection: bool = False


@dataclass(frozen=True, slots=True)
class RecordedRequest:
    """Bounded, redacted request record.

    ``method``, ``path``, ``headers``, and ``body`` exclude credential text.
    ``has_credential_in_slot`` checks the configured header;
    ``has_credential_outside_slot`` detects a leak elsewhere.
    """

    method: str
    path: str
    headers: dict[str, str]
    body: bytes
    has_credential_in_slot: bool
    has_credential_outside_slot: bool


class _ReplyStream(httpx.AsyncByteStream):
    def __init__(self, reply: FakeReply) -> None:
        self._reply = reply

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield self._reply.body
        for chunk in self._reply.stream_chunks:
            if chunk.delay_seconds:
                await asyncio.sleep(chunk.delay_seconds)
            yield chunk.data


class FakeProvider:
    """Queue responses and record sanitized requests for a caller-owned HTTP client.

    ``client()`` returns a new ``httpx.AsyncClient`` that the test must close.
    Enqueue replies before invoking a Query. An empty queue returns HTTP 500.
    This fixture is for ordered, single-task tests; concurrent requests require
    separate instances.
    """

    def __init__(self, credential_header: CredentialHeader, credential: str) -> None:
        if not credential_header.name or not credential:
            raise ValueError("fake provider requires a credential header and credential")
        self._credential_header = credential_header
        self._credential = credential.encode()
        self._replies: list[FakeReply] = []
        self._requests: list[RecordedRequest] = []

    def client(self) -> httpx.AsyncClient:
        """Return a caller-owned HTTP client connected to this scripted provider."""
        return httpx.AsyncClient(transport=httpx.MockTransport(self._respond))

    def enqueue_replies(self, *replies: FakeReply) -> None:
        """Append replies in request order."""
        self._replies.extend(replies)

    def requests(self) -> tuple[RecordedRequest, ...]:
        """Return sanitized records in arrival order."""
        return tuple(self._requests)

    async def _respond(self, request: httpx.Request) -> httpx.Response:
        body = (await request.aread())[:_MAX_RECORDED_REQUEST_BYTES]
        credential_header = self._credential_header.name.lower()
        expected = self._credential_header.prefix.encode() + self._credential
        slot = request.headers.get(credential_header, "").encode()
        other_headers = {
            name: value for name, value in request.headers.items() if name.lower() != credential_header
        }
        path = request.url.raw_path
        outside = self._credential in path or self._credential in body or any(
            self._credential in name.encode() or self._credential in value.encode()
            for name, value in other_headers.items()
        )
        self._requests.append(
            RecordedRequest(
                request.method,
                path.replace(self._credential, _REDACTED).decode(errors="replace"),
                {
                    name.replace(self._credential.decode(), _REDACTED.decode()): value.replace(
                        self._credential.decode(), _REDACTED.decode()
                    )
                    for name, value in other_headers.items()
                },
                body.replace(self._credential, _REDACTED),
                slot == expected,
                outside,
            )
        )
        reply = self._replies.pop(0) if self._replies else FakeReply(
            500, {"content-type": "application/json"}, b'{"error":{"type":"llmtest_no_scripted_reply"}}'
        )
        if reply.delay_seconds:
            await asyncio.sleep(reply.delay_seconds)
        if reply.should_drop_connection:
            raise httpx.ConnectError("fake provider dropped connection", request=request)
        if reply.stream_chunks:
            return httpx.Response(
                reply.status_code, headers=reply.headers, stream=_ReplyStream(reply), request=request
            )
        return httpx.Response(reply.status_code, headers=reply.headers, content=reply.body, request=request)


async def run_text_generation_exchange_suite(
    new_query: Callable[[httpx.AsyncClient], TextGenerationQuery],
    call: Call,
    request: TextGenerationRequest,
    credential_header: CredentialHeader,
    credential: str,
    generated_reply: FakeReply,
    malformed_reply: FakeReply,
) -> None:
    """Check generated, malformed, retry, and credential-isolation exchanges.

    ``new_query`` configures the connector against the supplied client and an
    HTTPS fake base URL. Each case uses the same Query and call identity; the
    fixture verifies that the credential appears only in its declared header.
    The consuming connector should add its own branch and schema cases.
    """
    provider = FakeProvider(credential_header, credential)
    provider.enqueue_replies(
        generated_reply,
        malformed_reply,
        FakeReply(503, {"content-type": "text/plain"}, b"service unavailable"),
    )
    async with provider.client() as client:
        query = new_query(client)
        generated = await query.invoke(call, request)
        malformed = await query.invoke(call, request)
        retry = await query.invoke(call, request)
    assert generated.branch == "generated"
    assert malformed.branch == "invalidResponse"
    assert retry.kind == "retry"
    assert len(provider.requests()) == 3
    assert all(
        record.has_credential_in_slot and not record.has_credential_outside_slot
        for record in provider.requests()
    )
