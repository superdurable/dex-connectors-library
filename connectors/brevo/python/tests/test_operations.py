# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Brevo contact operations."""

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

from dex_connectors.brevo import (
    CREATE_OR_UPDATE_CONTACT_DEFINITION,
    DEFECT,
    FOUND,
    GET_CONTACT_DEFINITION,
    INVALID_RESPONSE,
    NOT_FOUND,
    PROVIDER_REJECTED,
    SAVED,
    Client,
    Config,
    CreateOrUpdateContactInput,
    Credentials,
    GetContactInput,
)

CONNECTION = ConnectionRef("brevo", "brevo")
KEY = "xkeysib-test"


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, key: str = KEY
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848/v3", max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(key))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def upsert_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, CREATE_OR_UPDATE_CONTACT_DEFINITION.operation)


def get_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_CONTACT_DEFINITION.operation)


@pytest.mark.asyncio
async def test_created_contact_returns_new_id() -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(201, json={"id": 21}), requests)
    result = await run_mutation(
        client.create_or_update_contact(),
        upsert_call(),
        CreateOrUpdateContactInput("a@example.test", {"FNAME": "Ada"}),
    )
    assert result.branch == SAVED
    assert (result.value.id, result.value.created, result.value.email) == (21, True, "a@example.test")
    assert json.loads(requests[0].content) == {
        "email": "a@example.test",
        "attributes": {"FNAME": "Ada"},
        "updateEnabled": True,
    }
    assert requests[0].headers["api-key"] == KEY


@pytest.mark.asyncio
async def test_updated_contact_has_no_id() -> None:
    result = await run_mutation(
        make_client(httpx.Response(204)).create_or_update_contact(),
        upsert_call(),
        CreateOrUpdateContactInput("a@example.test"),
    )
    assert result.branch == SAVED
    assert (result.value.id, result.value.created) == (0, False)


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch"),
    [
        (httpx.Response(201, json={"id": 0}), INVALID_RESPONSE),
        (httpx.Response(201, content=b"x" * 400), INVALID_RESPONSE),
        (httpx.Response(200), PROVIDER_REJECTED),
        (httpx.Response(400), PROVIDER_REJECTED),
    ],
)
async def test_upsert_branches(response: httpx.Response, branch: str) -> None:
    result = await run_mutation(
        make_client(response).create_or_update_contact(),
        upsert_call(),
        CreateOrUpdateContactInput("a@example.test"),
    )
    assert result.branch == branch


@pytest.mark.asyncio
async def test_lookup_escapes_email_and_validates_contact() -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json={"id": 21, "email": "a+b@example.test"}), requests)
    result = await run_query(client.get_contact(), get_call(), GetContactInput("a+b@example.test"))
    assert result.branch == FOUND
    assert requests[0].url.raw_path == b"/v3/contacts/a%2Bb%40example.test"
    invalid = await run_query(
        make_client(httpx.Response(200, json={"id": 21, "email": "bad"})).get_contact(),
        get_call(),
        GetContactInput("a@x.test"),
    )
    assert invalid.branch == INVALID_RESPONSE


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "branch", "kind"),
    [
        (404, NOT_FOUND, FailureKind.NOT_FOUND),
        (401, PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (403, PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
    ],
)
async def test_lookup_status_branches(status: int, branch: str, kind: FailureKind) -> None:
    result = await run_query(
        make_client(httpx.Response(status)).get_contact(), get_call(), GetContactInput("a@x.test")
    )
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [408, 409, 429, 500])
async def test_safe_failures_retry(status: int) -> None:
    with pytest.raises(RetryError):
        await run_mutation(
            make_client(httpx.Response(status)).create_or_update_contact(),
            upsert_call(),
            CreateOrUpdateContactInput("a@x.test"),
        )


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [
        CreateOrUpdateContactInput("Ada <a@x.test>"),
        CreateOrUpdateContactInput("a@x.test", {"fname": "Ada"}),
        CreateOrUpdateContactInput(""),
    ],
)
async def test_invalid_upsert_is_defect_without_request(value: CreateOrUpdateContactInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_mutation(
        make_client(httpx.Response(201), requests).create_or_update_contact(), upsert_call(), value
    )
    assert result.branch == DEFECT
    assert requests == []
