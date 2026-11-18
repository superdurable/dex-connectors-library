# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Confluence footer comments whose outcome is unknown."""

import asyncio
from collections.abc import AsyncIterator, Callable
from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.singledispatch import Marker

import dex_connectors.atlassian_confluence as confluence
from dex_connectors.atlassian_confluence._generated.definitions import ADD_COMMENT_DEFINITION

CONNECTION = sdk.ConnectionRef("atlassian", "confluence-site")
CALL_ID = sdk.CallID("1d4a9b66-087a-52c9-aee1-005513d3b6ac")
COMMENT = confluence.AddCommentInput(page_id="600002", body="Approved", body_format="plainText")


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self, attempt: int = 1, heartbeat_value: object = None) -> None:
        self.attempt = attempt
        self.run_id = "run"
        self.first_attempt_at = datetime.now(UTC)
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value = heartbeat_value
        self.heartbeat_read_error: Exception | None = None

    @property
    def marker(self) -> Marker | None:
        """Decode this call's checkpoint from either SDK heartbeat format."""
        if isinstance(self.heartbeat_value, Marker) or self.heartbeat_value is None:
            return self.heartbeat_value
        assert isinstance(self.heartbeat_value, dict)
        entry = self.heartbeat_value["singleDispatchMarkers"].get(str(CALL_ID))
        return None if entry is None else Marker.from_wire(entry)

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None or self.heartbeat_read_error is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last heartbeat as the reading SDK expects it."""
        if self.heartbeat_read_error is not None:
            raise self.heartbeat_read_error
        seeded = self.heartbeat_value
        if isinstance(seeded, Marker) and value_type is not Marker:
            return {"singleDispatchMarkers": {str(seeded.call_id): seeded.to_wire()}}
        if isinstance(seeded, dict) and value_type is Marker:
            return self.marker
        return seeded

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        self.heartbeat_value = value


async def run(
    post_status: int, context: AttemptContext, requests: list[httpx.Request], *, max_sends: int = 3
) -> str:
    """Run one Execute attempt of addComment; read-back finds no comment."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.method == "GET":
            return httpx.Response(200, json={"results": []})
        return httpx.Response(post_status, json={"id": "700001"} if post_status < 300 else {})

    client = confluence.Client(
        confluence.Config(cloud_id="11223344-a1b2-4b33-8c44-def123456789", endpoint="http://127.0.0.1"),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: confluence.Credentials(
                    "client", sdk.SecretString("secret"), sdk.SecretString("token"), sdk.SecretString("")
                )
            }
        ),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )
    operation = sdk.OperationRef("confluence", "addComment")
    call = sdk.Call(CALL_ID, CONNECTION, operation, context=context, max_sends=max_sends)
    try:
        return (await sdk.run_mutation(client.add_comment(), call, COMMENT)).branch
    except sdk.RetryError:
        return "retry"


def posts(requests: list[httpx.Request]) -> int:
    return sum(1 for request in requests if request.method == "POST")


@pytest.mark.asyncio
async def test_unknown_outcome_reads_back_and_resends_below_the_cap_then_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, ADD_COMMENT_DEFINITION.max_sends + 1):
        context.attempt = attempt
        assert await run(500, context, requests) == "retry"
    assert posts(requests) == ADD_COMMENT_DEFINITION.max_sends
    assert sum(1 for request in requests if request.method == "GET") == ADD_COMMENT_DEFINITION.max_sends - 1
    context.attempt += 1
    assert await run(500, context, requests) == "uncertain"
    assert posts(requests) == ADD_COMMENT_DEFINITION.max_sends


@pytest.mark.asyncio
async def test_send_cap_of_one_reads_back_instead_of_resending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run(500, context, requests, max_sends=1) == "retry"
    context.attempt = 2
    assert await run(500, context, requests, max_sends=1) == "uncertain"
    assert posts(requests) == 1


@pytest.mark.asyncio
async def test_definite_refusal_records_can_resend_and_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run(403, context, requests) == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"
    assert posts(requests) == 1


@pytest.mark.asyncio
async def test_refusal_of_a_resend_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run(502, context, requests) == "retry"
    context.attempt = 2
    assert await run(400, context, requests) == "uncertain"
    assert posts(requests) == 2


@pytest.mark.asyncio
async def test_unreadable_checkpoint_retries_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext(2)
    context.heartbeat_read_error = RuntimeError("heartbeat store unavailable")
    assert await run(201, context, requests) == "retry"
    assert requests == []


class BrokenBody(httpx.AsyncByteStream):
    """A response body that fails after the status line arrived."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"id":"70'
        raise httpx.ReadError("connection reset")


def matching_comment(comment_id: str) -> dict[str, object]:
    """Render a footer comment with the requested text, created now."""
    return {
        "id": comment_id,
        "pageId": "600002",
        "version": {"number": 1, "createdAt": datetime.now(UTC).strftime("%Y-%m-%dT%H:%M:%S.000Z")},
        "body": {"storage": {"representation": "storage", "value": "<p>Approved</p>"}},
    }


async def run_with(
    respond: Callable[[httpx.Request], httpx.Response],
    context: AttemptContext,
    credentials: object | None = None,
) -> sdk.MutationResult[confluence.AddCommentOutput] | str:
    """Run one Execute attempt of addComment against an arbitrary provider."""
    client = confluence.Client(
        confluence.Config(cloud_id="11223344-a1b2-4b33-8c44-def123456789", endpoint="http://127.0.0.1"),
        credentials
        or sdk.StaticCredentialProvider(
            {
                CONNECTION: confluence.Credentials(
                    "client", sdk.SecretString("secret"), sdk.SecretString("token"), sdk.SecretString("")
                )
            }
        ),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )
    operation = sdk.OperationRef("confluence", "addComment")
    call = sdk.Call(CALL_ID, CONNECTION, operation, context=context, max_sends=3)
    try:
        return await sdk.run_mutation(client.add_comment(), call, COMMENT)
    except sdk.RetryError as error:
        return "retry:" + str(error.failure.kind)


@pytest.mark.asyncio
async def test_service_unavailable_records_can_resend() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run(503, context, requests) == "retry"
    assert context.marker is not None and context.marker.state == "canResend"


@pytest.mark.asyncio
async def test_rate_limited_resend_reads_back_before_the_next_send() -> None:
    requests: list[httpx.Request] = []
    statuses = [500, 429]

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.method == "POST":
            return httpx.Response(statuses.pop(0), json={})
        reads = sum(1 for sent in requests if sent.method == "GET")
        return httpx.Response(200, json={"results": [] if reads == 1 else [matching_comment("700009")]})

    context = AttemptContext()
    assert await run_with(respond, context) == "retry:AVAILABILITY"
    context.attempt = 2
    assert await run_with(respond, context) == "retry:RATE_LIMIT"
    assert context.marker is not None and context.marker.state == "canResend"
    context.attempt = 3
    result = await run_with(respond, context)
    assert not isinstance(result, str) and result.branch == "added"
    assert result.value.comment_id == "700009"
    assert posts(requests) == 2


@pytest.mark.asyncio
async def test_read_back_of_a_missing_page_after_a_possible_send_is_uncertain() -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(404, json={})

    result = await run_with(respond, AttemptContext(2))
    assert not isinstance(result, str) and result.branch == "uncertain"
    assert posts(requests) == 0


@pytest.mark.asyncio
@pytest.mark.parametrize(("is_visible", "branch"), [(True, "added"), (False, "uncertain")])
async def test_accepted_comment_with_a_broken_body_is_never_resent(is_visible: bool, branch: str) -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.method == "POST":
            return httpx.Response(201, stream=BrokenBody())
        return httpx.Response(200, json={"results": [matching_comment("700010")] if is_visible else []})

    result = await run_with(respond, AttemptContext())
    assert not isinstance(result, str) and result.branch == branch
    assert posts(requests) == 1


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [400, 404])
async def test_refusal_with_an_oversized_body_is_unknown(status: int) -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(status, content=b"x" * (70 << 10))

    context = AttemptContext()
    assert await run_with(respond, context) == "retry:TRANSPORT"
    assert context.marker is not None and context.marker.state == "claimed"
    assert posts(requests) == 1


class _FailingCredentials:
    """Fail every resolution with one error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> confluence.Credentials:
        raise self.error


@pytest.mark.asyncio
async def test_credential_failures_retry_when_transient_and_are_uncertain_after_a_possible_send() -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(500)

    first = AttemptContext()
    outage = _FailingCredentials(OSError("credential broker is unavailable"))
    assert await run_with(respond, first, outage) == "retry:AVAILABILITY"
    assert first.marker is not None and first.marker.state == "notSent"
    reauthorize = _FailingCredentials(sdk.ReauthorizationRequiredError())
    terminal = await run_with(respond, AttemptContext(), reauthorize)
    assert not isinstance(terminal, str) and terminal.branch == "defect"

    context = AttemptContext()
    assert await run_with(respond, context) == "retry:AVAILABILITY"
    claim = context.heartbeat_value
    context.attempt = 2
    assert await run_with(respond, context, outage) == "retry:AVAILABILITY"
    assert context.heartbeat_value is claim, "notSent would hide the earlier possible send"
    after_send = await run_with(respond, context, reauthorize)
    assert not isinstance(after_send, str) and after_send.branch == "uncertain"
    assert posts(requests) == 1


class _SlowAcceptedBody(httpx.AsyncByteStream):
    """Send 2xx headers, then hold the body past the write window."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        await asyncio.sleep(30)
        yield b"{}"


@pytest.mark.asyncio
@pytest.mark.parametrize(("is_visible", "branch"), [(True, "added"), (False, "uncertain")])
async def test_write_deadline_after_accepted_headers_is_never_resent(is_visible: bool, branch: str) -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.method == "POST":
            return httpx.Response(201, stream=_SlowAcceptedBody())
        return httpx.Response(200, json={"results": [matching_comment("700010")] if is_visible else []})

    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(seconds=4)
    result = await run_with(respond, context)
    assert not isinstance(result, str) and result.branch == branch
    assert posts(requests) == 1
