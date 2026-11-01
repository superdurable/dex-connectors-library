# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Klaviyo operations."""

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

from dex_connectors.klaviyo import (
    ACCEPTED,
    API_REVISION,
    CREATE_EVENT_DEFINITION,
    DEFECT,
    FOUND,
    GET_PROFILE_DEFINITION,
    INVALID_RESPONSE,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    CreateEventInput,
    Credentials,
    GetProfileInput,
)

CONNECTION = ConnectionRef("klaviyo", "klaviyo")
KEY = "pk_test"
PROFILE = {"data": {"type": "profile", "id": "p1", "attributes": {"email": "a@example.test"}}}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, key: str = KEY
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848/api", max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(key))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def profile_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_PROFILE_DEFINITION.operation)


def event_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, CREATE_EVENT_DEFINITION.operation)


@pytest.mark.asyncio
async def test_profile_uses_api_key_and_revision() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=PROFILE), requests).get_profile(),
        profile_call(),
        GetProfileInput("p1"),
    )
    assert result.branch == FOUND
    assert result.value.email == "a@example.test"
    assert requests[0].url.raw_path == b"/api/profiles/p1"
    assert requests[0].headers["Authorization"] == "Klaviyo-API-Key " + KEY
    assert requests[0].headers["revision"] == API_REVISION


@pytest.mark.asyncio
async def test_event_carries_hashed_call_id_as_unique_id() -> None:
    requests: list[httpx.Request] = []
    call = event_call()
    client = make_client(httpx.Response(202), requests)
    result = await run_mutation(
        client.create_event(), call, CreateEventInput("a@example.test", "Signed Up", {"plan": "pro"})
    )
    assert result.branch == ACCEPTED
    assert result.value.accepted is True
    attributes = json.loads(requests[0].content)["data"]["attributes"]
    assert attributes["unique_id"] == hashlib.sha256(str(call.id).encode()).hexdigest()
    assert attributes["properties"] == {"plan": "pro"}
    assert attributes["metric"]["data"]["attributes"]["name"] == "Signed Up"
    assert attributes["profile"]["data"]["attributes"]["email"] == "a@example.test"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(404), NOT_FOUND, FailureKind.NOT_FOUND),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (
            httpx.Response(200, json={"data": {"type": "event", "id": "x"}}),
            INVALID_RESPONSE,
            FailureKind.PROTOCOL,
        ),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_profile_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(make_client(response).get_profile(), profile_call(), GetProfileInput("p1"))
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "branch"), [(200, PROVIDER_REJECTED), (400, PROVIDER_REJECTED), (401, PROVIDER_REJECTED)]
)
async def test_event_needs_202(status: int, branch: str) -> None:
    result = await run_mutation(
        make_client(httpx.Response(status)).create_event(),
        event_call(),
        CreateEventInput("a@example.test", "m"),
    )
    assert result.branch == branch


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [408, 409, 429, 500])
async def test_safe_failures_retry(status: int) -> None:
    with pytest.raises(RetryError):
        await run_mutation(
            make_client(httpx.Response(status)).create_event(),
            event_call(),
            CreateEventInput("a@example.test", "m"),
        )


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [
        CreateEventInput("not-an-email", "m"),
        CreateEventInput("a@example.test", " "),
        CreateEventInput("a b@x.test", "m"),
    ],
)
async def test_invalid_event_is_defect_without_request(value: CreateEventInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_mutation(
        make_client(httpx.Response(202), requests).create_event(), event_call(), value
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize("profile_id", ["", "a/b", "a?b"])
async def test_invalid_profile_id_and_key_are_defects_without_request(profile_id: str) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests).get_profile(), profile_call(), GetProfileInput(profile_id)
    )
    assert result.branch == DEFECT
    blank_key = await run_query(
        make_client(httpx.Response(200), requests, key="").get_profile(),
        profile_call(),
        GetProfileInput("p1"),
    )
    assert blank_key.branch == DEFECT
    assert requests == []
