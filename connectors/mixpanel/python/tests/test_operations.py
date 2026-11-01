# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Mixpanel operations."""

import base64
import hashlib
import json
from uuid import uuid4

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_mutation,
    run_query,
)

from dex_connectors.mixpanel import (
    DEFECT,
    EXPORT_EVENTS_DEFINITION,
    EXPORTED,
    IMPORTED,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    TRACK_EVENT_DEFINITION,
    Client,
    Config,
    Credentials,
    ExportEventsInput,
    TrackEventInput,
)

CONNECTION = ConnectionRef("mixpanel", "mixpanel")
IMPORT_OK = {"code": 200, "num_records_imported": 1, "status": "OK"}
EVENT = TrackEventInput("Signed Up", "member-1", 1793232000, {"plan": "pro"})
EXPORT = ExportEventsInput("2026-10-01", "2026-10-02", 2)


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, secret: str = "secret"
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(
            42,
            base_url="http://localhost:8848",
            export_base_url="http://localhost:8849/api/2.0",
            max_response_bytes=400,
        ),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString("sa.user"), SecretString(secret))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def track_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, TRACK_EVENT_DEFINITION.operation)


def export_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, EXPORT_EVENTS_DEFINITION.operation)


@pytest.mark.asyncio
async def test_track_event_sends_stable_deduplication_tuple() -> None:
    requests: list[httpx.Request] = []
    call = track_call()
    result = await run_mutation(
        make_client(httpx.Response(200, json=IMPORT_OK), requests).track_event(), call, EVENT
    )
    assert result.branch == IMPORTED
    assert result.value.imported is True
    request = requests[0]
    assert request.method == "POST"
    assert request.url.path == "/import"
    assert dict(request.url.params) == {"project_id": "42", "strict": "1"}
    assert request.headers["Authorization"] == "Basic " + base64.b64encode(b"sa.user:secret").decode()
    assert request.headers["Content-Type"] == "application/json"
    digest = hashlib.sha256(str(call.id).encode()).hexdigest()
    insert_id = f"{digest[:8]}-{digest[8:12]}-{digest[12:16]}-{digest[16:20]}-{digest[20:32]}"
    assert json.loads(request.content) == [
        {
            "event": "Signed Up",
            "properties": {
                "time": 1793232000,
                "distinct_id": "member-1",
                "$insert_id": insert_id,
                "ip": 0,
                "plan": "pro",
            },
        }
    ]


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [
        TrackEventInput(" ", "member-1", 1),
        TrackEventInput("Signed Up", "", 1),
        TrackEventInput("Signed Up", "member-1", 0),
        TrackEventInput("Signed Up", "member-1", 1, {"distinct_id": "x"}),
        TrackEventInput("Signed Up", "member-1", 1, {" ": "x"}),
        TrackEventInput("Signed Up", "member-1", 1, {"big": "x" * (1 << 20)}),
    ],
)
async def test_track_event_rejects_invalid_input_before_dispatch(value: TrackEventInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_mutation(
        make_client(httpx.Response(200, json=IMPORT_OK), requests).track_event(), track_call(), value
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.VALIDATION
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(400), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (
            httpx.Response(200, json={**IMPORT_OK, "num_records_imported": 0}),
            INVALID_RESPONSE,
            FailureKind.PROTOCOL,
        ),
        (httpx.Response(200, content=b"x" * 500), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_track_event_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_mutation(make_client(response).track_event(), track_call(), EVENT)
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
async def test_export_events_decodes_jsonl_without_arbitrary_properties() -> None:
    requests: list[httpx.Request] = []
    body = (
        b'{"event":"Signed Up","properties":{"distinct_id":"member-1","time":1793232000,'
        b'"$insert_id":"abc","email":"secret@example.test"}}\n\n'
        b'{"event":"Logged In","properties":{"distinct_id":"member-2","time":1793232001}}\n'
    )
    result = await run_query(
        make_client(httpx.Response(200, content=body), requests).export_events(), export_call(), EXPORT
    )
    assert result.branch == EXPORTED
    assert [
        (event.event, event.distinct_id, event.time, event.insert_id) for event in result.value.events
    ] == [
        ("Signed Up", "member-1", 1793232000, "abc"),
        ("Logged In", "member-2", 1793232001, ""),
    ]
    request = requests[0]
    assert request.method == "GET"
    assert request.url.path == "/api/2.0/export"
    assert dict(request.url.params) == {
        "project_id": "42",
        "from_date": "2026-10-01",
        "to_date": "2026-10-02",
        "limit": "2",
    }
    assert request.headers["Accept"] == "text/plain"


@pytest.mark.asyncio
async def test_export_events_empty_body_is_exported() -> None:
    result = await run_query(
        make_client(httpx.Response(200, content=b"")).export_events(), export_call(), EXPORT
    )
    assert result.branch == EXPORTED
    assert result.value.events == ()


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [
        ExportEventsInput("2026-10-02", "2026-10-01", 1),
        ExportEventsInput("2026-10-01", "2026-11-01", 1),
        ExportEventsInput("20261001", "2026-10-01", 1),
        ExportEventsInput("2026-W40-1", "2026-10-01", 1),
        ExportEventsInput("2026-10-01", "2026-10-01", 0),
        ExportEventsInput("2026-10-01", "2026-10-01", 101),
    ],
)
async def test_export_events_rejects_invalid_input_before_dispatch(value: ExportEventsInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(make_client(httpx.Response(200), requests).export_events(), export_call(), value)
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(200, content=b'{"properties":{}}\n'), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"not json\n"), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (
            httpx.Response(200, content=b'{"event":"a"}\n{"event":"b"}\n{"event":"c"}\n'),
            INVALID_RESPONSE,
            FailureKind.PROTOCOL,
        ),
        (httpx.Response(200, content=b"x" * 500), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_export_events_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(make_client(response).export_events(), export_call(), EXPORT)
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [408, 409, 429, 500, 503])
async def test_safe_failures_retry(status: int) -> None:
    with pytest.raises(RetryError):
        await run_mutation(make_client(httpx.Response(status)).track_event(), track_call(), EVENT)
    with pytest.raises(RetryError):
        await run_query(make_client(httpx.Response(status)).export_events(), export_call(), EXPORT)


@pytest.mark.asyncio
async def test_transport_failure_retries() -> None:
    def fail(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("refused", request=request)

    client = Client(
        Config(42, base_url="http://localhost:8848"),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString("u"), SecretString("s"))}),
        httpx.AsyncClient(transport=httpx.MockTransport(fail)),
    )
    with pytest.raises(RetryError) as raised:
        await run_mutation(client.track_event(), track_call(), EVENT)
    assert raised.value.failure.kind == FailureKind.TRANSPORT


@pytest.mark.asyncio
async def test_blank_secret_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests, secret="").export_events(), export_call(), EXPORT
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHENTICATION
    assert requests == []


def test_project_id_must_be_positive() -> None:
    with pytest.raises(ValueError):
        Client(Config(0), StaticCredentialProvider({}))
