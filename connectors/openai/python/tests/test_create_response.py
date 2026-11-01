# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Checkpoint fencing, streaming, and read-back parity for createResponse."""

import asyncio
import json
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from typing import Any
from uuid import uuid4

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    MutationResult,
    Progress,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_mutation,
)

import dex_connectors.openai.client as openai_client
from dex_connectors.openai import (
    COMPLETED,
    CREATE_RESPONSE_DEFINITION,
    DEFECT,
    FAILED,
    PROVIDER_REJECTED,
    UNCERTAIN,
    Client,
    Config,
    CreateRequest,
    Credentials,
    Response,
    StructuredOutput,
)
from dex_connectors.openai.client import CreateResponseCheckpoint

CONNECTION = ConnectionRef("openai", "openai")
API_KEY = "sk-test-0123456789"
RESPONSE_ID = "resp_abc123"
REQUEST = CreateRequest(input="Summarize this.", instructions="Be brief.")


def wire(status: str = "completed", response_id: str = RESPONSE_ID, text: str = "Done.") -> dict[str, Any]:
    return {
        "id": response_id,
        "model": "gpt-6-sol",
        "status": status,
        "output": [
            {"type": "message", "content": [{"type": "output_text", "text": text}, {"type": "refusal"}]},
            {"type": "reasoning", "content": None},
        ],
        "usage": {
            "input_tokens": 10,
            "output_tokens": 3,
            "total_tokens": 13,
            "input_tokens_details": {"cached_tokens": 2},
            "output_tokens_details": {"reasoning_tokens": 1},
        },
    }


@dataclass
class FakeContext:
    attempt: int = 1
    previous: Any = None
    deadline: datetime | None = None
    heartbeats: list[CreateResponseCheckpoint] = field(default_factory=list)

    def get_last_heartbeat_value(self, value_type: type[Any]) -> Any:
        if isinstance(self.previous, Exception):
            raise self.previous
        return self.previous

    async def heartbeat(self, value: object = None) -> None:
        assert isinstance(value, CreateResponseCheckpoint)
        self.heartbeats.append(value)


@dataclass
class TextCollector:
    texts: list[str] = field(default_factory=list)

    def write(self, value: str) -> None:
        self.texts.append(value)


@dataclass
class ProgressCollector:
    phases: list[Progress] = field(default_factory=list)

    def report(self, progress: Progress) -> None:
        self.phases.append(progress)


def make_call(
    context: FakeContext, text: TextCollector | None = None, progress: ProgressCollector | None = None
) -> Call:
    return Call(
        CallID(str(uuid4())),
        CONNECTION,
        CREATE_RESPONSE_DEFINITION.operation,
        context=context,  # type: ignore[arg-type]
        _progress_reporter=progress,  # type: ignore[arg-type]
        _text_writer=text,  # type: ignore[arg-type]
    )


def make_client(
    handler: Callable[[httpx.Request], httpx.Response], requests: list[httpx.Request], **config: Any
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return handler(request)

    return Client(
        Config(endpoint="http://localhost:8848/v1", **config),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(API_KEY))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


async def create(client: Client, call: Call, request: CreateRequest = REQUEST) -> MutationResult[Response]:
    return await run_mutation(client.create_response(), call, request)


@pytest.fixture(autouse=True)
def fast_read_back(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(openai_client, "_READ_BACK_INITIAL_INTERVAL_SECONDS", 0.01)
    monkeypatch.setattr(openai_client, "_READ_BACK_MAX_INTERVAL_SECONDS", 0.02)


@pytest.mark.asyncio
async def test_first_attempt_sends_payload_and_checkpoints_response_id() -> None:
    requests: list[httpx.Request] = []
    client = make_client(
        lambda request: httpx.Response(200, json=wire(), headers={"X-Request-Id": "req_1"}), requests
    )
    context = FakeContext()
    call = make_call(context)
    structured = CreateRequest(
        input=[{"role": "user", "content": "hi"}],
        model="gpt-6-luna",
        structured_output=StructuredOutput("summary", {"type": "object"}, strict=True),
    )
    result = await create(client, call, structured)
    assert result.branch == COMPLETED
    assert result.value == Response(RESPONSE_ID, "gpt-6-sol", "completed", "Done.", result.value.usage)
    assert (result.value.usage.cached_input_tokens, result.value.usage.reasoning_output_tokens) == (2, 1)
    assert result.receipt.provider_object_id == RESPONSE_ID
    assert result.receipt.provider_request_id == "req_1"
    request = requests[0]
    assert request.method == "POST" and request.url.path == "/v1/responses"
    assert request.headers["Authorization"] == "Bearer " + API_KEY
    assert request.headers["X-Client-Request-Id"] == str(call.id)
    assert json.loads(request.content) == {
        "model": "gpt-6-luna",
        "input": [{"role": "user", "content": "hi"}],
        "text": {
            "format": {
                "type": "json_schema",
                "name": "summary",
                "description": "",
                "schema": {"type": "object"},
                "strict": True,
            }
        },
    }
    assert context.heartbeats[0] == CreateResponseCheckpoint(str(call.id), 1)
    assert context.heartbeats[-1] == CreateResponseCheckpoint(str(call.id), 1, RESPONSE_ID)


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("request_value", "message"),
    [
        (CreateRequest(input=None), openai_client.INVALID_CREATE_INPUT_MESSAGE),
        (CreateRequest(input="x", model="bad model"), openai_client.INVALID_CREATE_INPUT_MESSAGE),
        (CreateRequest(input={"value": object()}), openai_client.NOT_SERIALIZABLE_MESSAGE),
    ],
)
async def test_invalid_requests_are_defects_without_sending(
    request_value: CreateRequest, message: str
) -> None:
    requests: list[httpx.Request] = []
    client = make_client(lambda request: httpx.Response(200, json=wire()), requests)
    result = await create(client, make_call(FakeContext()), request_value)
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.message == message
    assert requests == []


@pytest.mark.asyncio
async def test_retryable_status_marks_checkpoint_resendable() -> None:
    requests: list[httpx.Request] = []
    client = make_client(lambda request: httpx.Response(503), requests)
    context = FakeContext()
    call = make_call(context)
    with pytest.raises(RetryError):
        await create(client, call)
    assert context.heartbeats[-1] == CreateResponseCheckpoint(str(call.id), 1, can_resend=True)


@pytest.mark.asyncio
async def test_rate_limit_retries_with_retry_after() -> None:
    client = make_client(lambda request: httpx.Response(429, headers={"Retry-After": "4"}), [])
    with pytest.raises(RetryError) as raised:
        await create(client, make_call(FakeContext()))
    assert raised.value.failure.kind == FailureKind.RATE_LIMIT
    assert raised.value.retry_after_whole_seconds == 4


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "branch", "kind"),
    [
        (500, UNCERTAIN, FailureKind.AVAILABILITY),
        (408, UNCERTAIN, FailureKind.TRANSPORT),
        (409, UNCERTAIN, FailureKind.CONFLICT),
        (400, PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (401, PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (403, PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
    ],
)
async def test_terminal_create_statuses(status: int, branch: str, kind: FailureKind) -> None:
    client = make_client(lambda request: httpx.Response(status), [])
    result = await create(client, make_call(FakeContext()))
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
async def test_ambiguous_transport_failure_is_uncertain() -> None:
    def fail(request: httpx.Request) -> httpx.Response:
        raise httpx.ReadError("reset", request=request)

    result = await create(make_client(fail, []), make_call(FakeContext()))
    assert result.branch == UNCERTAIN


@pytest.mark.asyncio
async def test_connection_refused_before_send_retries_and_allows_resend() -> None:
    client = Client(
        Config(endpoint="http://127.0.0.1:1/v1"),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(API_KEY))}),
    )
    context = FakeContext()
    call = make_call(context)
    with pytest.raises(RetryError):
        await create(client, call)
    assert context.heartbeats[-1].can_resend is True
    await client.aclose()


@pytest.mark.asyncio
async def test_saved_response_id_reads_back_instead_of_sending() -> None:
    requests: list[httpx.Request] = []
    client = make_client(lambda request: httpx.Response(200, json=wire("failed")), requests)
    context = FakeContext(attempt=2)
    call = make_call(context)
    context.previous = CreateResponseCheckpoint(str(call.id), 1, RESPONSE_ID)
    result = await create(client, call)
    assert result.branch == FAILED
    assert [request.method for request in requests] == ["GET"]
    assert requests[0].url.path == "/v1/responses/" + RESPONSE_ID


@pytest.mark.asyncio
async def test_resendable_previous_attempt_sends_again() -> None:
    requests: list[httpx.Request] = []
    client = make_client(lambda request: httpx.Response(200, json=wire()), requests)
    context = FakeContext(attempt=3)
    call = make_call(context)
    context.previous = CreateResponseCheckpoint(str(call.id), 2, can_resend=True)
    assert (await create(client, call)).branch == COMPLETED
    assert [request.method for request in requests] == ["POST"]


@pytest.mark.asyncio
@pytest.mark.parametrize("previous_attempt", [None, 1])
async def test_unfenced_retry_is_uncertain_without_sending(previous_attempt: int | None) -> None:
    requests: list[httpx.Request] = []
    client = make_client(lambda request: httpx.Response(200, json=wire()), requests)
    context = FakeContext(attempt=2)
    call = make_call(context)
    if previous_attempt is not None:
        context.previous = CreateResponseCheckpoint(str(call.id), previous_attempt)
    result = await create(client, call)
    assert result.branch == UNCERTAIN
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize("previous", [CreateResponseCheckpoint("other-call", 1), ValueError("foreign value")])
async def test_foreign_heartbeat_value_is_defect(previous: object) -> None:
    client = make_client(lambda request: httpx.Response(200, json=wire()), [])
    result = await create(client, make_call(FakeContext(attempt=2, previous=previous)))
    assert result.branch == DEFECT


@pytest.mark.asyncio
async def test_in_progress_response_is_polled_until_terminal() -> None:
    requests: list[httpx.Request] = []
    statuses = iter(["in_progress", "queued", "completed"])

    def respond(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(200, json=wire("queued"))
        return httpx.Response(200, json=wire(next(statuses)))

    result = await create(make_client(respond, requests), make_call(FakeContext()))
    assert result.branch == COMPLETED
    assert [request.method for request in requests] == ["POST", "GET", "GET", "GET"]


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("read_back", "kind"),
    [
        (httpx.Response(404), FailureKind.NOT_FOUND),
        (httpx.Response(403), FailureKind.AUTHORIZATION),
        (httpx.Response(400), FailureKind.PROVIDER_REJECTION),
        (httpx.Response(200, json=wire(response_id="resp_other")), FailureKind.PROTOCOL),
    ],
)
async def test_unconfirmable_read_back_is_uncertain(read_back: httpx.Response, kind: FailureKind) -> None:
    def respond(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json=wire("in_progress")) if request.method == "POST" else read_back

    result = await create(make_client(respond, []), make_call(FakeContext()))
    assert result.branch == UNCERTAIN
    assert result.failure is not None and result.failure.kind == kind
    assert result.value.id == RESPONSE_ID


@pytest.mark.asyncio
async def test_read_back_retries_when_deadline_leaves_no_time() -> None:
    def respond(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json=wire("in_progress"))

    context = FakeContext(deadline=datetime.now(UTC) + timedelta(seconds=5))
    with pytest.raises(RetryError):
        await create(make_client(respond, []), make_call(context))
    assert context.heartbeats[-1].response_id == RESPONSE_ID


@pytest.mark.asyncio
async def test_cancelled_response_retries_with_resend_checkpoint() -> None:
    context = FakeContext()
    with pytest.raises(RetryError):
        await create(
            make_client(lambda request: httpx.Response(200, json=wire("cancelled")), []), make_call(context)
        )
    assert context.heartbeats[-1].can_resend is True


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [{"model": "gpt", "status": "completed"}, {"id": "bad id!", "status": "completed"}, b"x" * 200, b"{"],
)
async def test_unusable_create_body_is_uncertain(body: object) -> None:
    def respond(request: httpx.Request) -> httpx.Response:
        if isinstance(body, bytes):
            return httpx.Response(200, content=body)
        return httpx.Response(200, json=body)

    result = await create(make_client(respond, [], max_response_bytes=100), make_call(FakeContext()))
    assert result.branch == UNCERTAIN


def sse(*events: dict[str, Any] | str) -> bytes:
    lines = []
    for event in events:
        data = event if isinstance(event, str) else json.dumps(event)
        lines.append(f"event: x\ndata: {data}\n\n")
    return "".join(lines).encode()


@pytest.mark.asyncio
async def test_stream_reports_progress_text_and_terminal_response() -> None:
    requests: list[httpx.Request] = []
    body = sse(
        {
            "type": "response.created",
            "sequence_number": 1,
            "response": {"id": RESPONSE_ID, "status": "queued"},
        },
        "not json",
        {"type": "response.output_text.delta", "delta": "Do"},
        {"type": "response.output_text.delta", "delta": "ne."},
        {"type": "response.completed", "response": wire()},
    )
    client = make_client(lambda request: httpx.Response(200, content=body), requests)
    context = FakeContext()
    text = TextCollector()
    progress = ProgressCollector()
    call = make_call(context, text, progress)
    result = await create(client, call)
    assert result.branch == COMPLETED
    assert result.value.output_text == "Done."
    assert text.texts == ["Do", "ne."]
    assert progress.phases == [Progress("response.created", "queued", provider_sequence=1)]
    assert json.loads(requests[0].content)["stream"] is True
    assert context.heartbeats[-1].response_id == RESPONSE_ID


@pytest.mark.asyncio
async def test_stream_failed_event_selects_failed() -> None:
    body = sse(
        {"type": "response.created", "response": {"id": RESPONSE_ID}},
        {"type": "response.incomplete", "response": wire("incomplete")},
    )
    client = make_client(lambda request: httpx.Response(200, content=body), [])
    result = await create(client, make_call(FakeContext(), TextCollector()))
    assert result.branch == FAILED


@pytest.mark.asyncio
async def test_stream_ending_after_id_reads_back() -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(
                200, content=sse({"type": "response.created", "response": {"id": RESPONSE_ID}})
            )
        return httpx.Response(200, json=wire())

    result = await create(make_client(respond, requests), make_call(FakeContext(), TextCollector()))
    assert result.branch == COMPLETED
    assert [request.method for request in requests] == ["POST", "GET"]


@pytest.mark.asyncio
async def test_stream_error_before_id_is_uncertain() -> None:
    body = sse({"type": "error", "message": "boom"})
    result = await create(
        make_client(lambda request: httpx.Response(200, content=body), []),
        make_call(FakeContext(), TextCollector()),
    )
    assert result.branch == UNCERTAIN
    assert result.failure is not None and "streamed an error" in result.failure.message


@pytest.mark.asyncio
async def test_oversized_stream_event_after_id_drains_and_reads_back() -> None:
    requests: list[httpx.Request] = []
    body = sse(
        {"type": "response.created", "response": {"id": RESPONSE_ID}},
        {"type": "response.output_text.delta", "delta": "x" * 500},
    )

    def respond(request: httpx.Request) -> httpx.Response:
        return (
            httpx.Response(200, content=body)
            if request.method == "POST"
            else httpx.Response(200, json=wire())
        )

    client = make_client(respond, requests, max_sse_event_bytes=200)
    result = await create(client, make_call(FakeContext(), TextCollector()))
    assert result.branch == COMPLETED
    assert [request.method for request in requests] == ["POST", "GET"]


@pytest.mark.asyncio
async def test_oversized_stream_before_id_is_uncertain() -> None:
    body = sse({"type": "response.output_text.delta", "delta": "x" * 500})
    client = make_client(lambda request: httpx.Response(200, content=body), [], max_response_bytes=200)
    result = await create(client, make_call(FakeContext(), TextCollector()))
    assert result.branch == UNCERTAIN
    assert result.failure is not None and result.failure.message == "stream exceeds the configured size limit"


@pytest.mark.asyncio
async def test_periodic_renewal_never_overwrites_newer_response_id(monkeypatch: pytest.MonkeyPatch) -> None:
    class SlowContext(FakeContext):
        async def heartbeat(self, value: object = None) -> None:
            await asyncio.sleep(0.005)
            await super().heartbeat(value)

    def respond(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json=wire("in_progress"))

    monkeypatch.setattr(openai_client, "_READ_BACK_INITIAL_INTERVAL_SECONDS", 0.02)
    context = SlowContext(deadline=datetime.now(UTC) + timedelta(seconds=5.3))
    client = make_client(respond, [])
    client.checkpoint_heartbeat_interval_seconds = 0.001
    with pytest.raises(RetryError):
        await create(client, make_call(context))
    response_ids = [checkpoint.response_id for checkpoint in context.heartbeats]
    first_with_id = response_ids.index(RESPONSE_ID)
    assert all(response_id == RESPONSE_ID for response_id in response_ids[first_with_id:])
    assert len(response_ids) > first_with_id + 3
