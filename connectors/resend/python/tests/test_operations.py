# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Resend send Mutation and lookup Query."""

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

from dex_connectors.resend import (
    DEFECT,
    FOUND,
    GET_EMAIL_DEFINITION,
    INVALID_RESPONSE,
    NOT_FOUND,
    PROVIDER_REJECTED,
    SEND_EMAIL_DEFINITION,
    SENT,
    Client,
    Config,
    Credentials,
    GetEmailInput,
    SendEmailInput,
)

CONNECTION = ConnectionRef("resend", "resend")
KEY = "re_test_0123456789abcdef"
EMAIL_ID = "5f9f777d-04e0-482a-bb65-088bb5bd44a4"
INPUT = SendEmailInput("Acme <hello@example.test>", ("reader@example.test",), "Welcome", "<p>Hi</p>")


def make_client(
    response: httpx.Response,
    requests: list[httpx.Request] | None = None,
    key: str = KEY,
    config: Config | None = None,
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        config or Config(base_url="http://localhost:8848"),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(key))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def send_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, SEND_EMAIL_DEFINITION.operation)


def get_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_EMAIL_DEFINITION.operation)


@pytest.mark.asyncio
async def test_send_email_posts_json_with_call_idempotency_key() -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json={"id": EMAIL_ID}), requests)
    call = send_call()
    result = await run_mutation(client.send_email(), call, INPUT)
    assert result.branch == SENT
    assert result.value.id == EMAIL_ID
    request = requests[0]
    assert request.method == "POST" and request.url.path == "/emails"
    assert request.headers["Authorization"] == "Bearer " + KEY
    assert request.headers["Idempotency-Key"] == str(call.id)
    assert json.loads(request.content) == {
        "from": "Acme <hello@example.test>",
        "to": ["reader@example.test"],
        "subject": "Welcome",
        "html": "<p>Hi</p>",
    }
    assert KEY not in str(request.url)


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "email_input",
    [
        SendEmailInput("", ("reader@example.test",), "Welcome", "<p>Hi</p>"),
        SendEmailInput("Acme <hello@example.test>", (), "Welcome", "<p>Hi</p>"),
        SendEmailInput("Acme <hello@example.test>", ("",), "Welcome", "<p>Hi</p>"),
        SendEmailInput("Acme <hello@example.test>", ("reader@example.test",), "", "<p>Hi</p>"),
    ],
)
async def test_send_email_rejects_incomplete_input_without_request(email_input: SendEmailInput) -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json={"id": EMAIL_ID}), requests)
    result = await run_mutation(client.send_email(), send_call(), email_input)
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.VALIDATION
    assert requests == []


@pytest.mark.asyncio
async def test_send_email_blank_key_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json={"id": EMAIL_ID}), requests, key=" ")
    result = await run_mutation(client.send_email(), send_call(), INPUT)
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHENTICATION
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "kind"),
    [
        (httpx.Response(429, headers={"Retry-After": "3"}), FailureKind.RATE_LIMIT),
        (httpx.Response(503), FailureKind.AVAILABILITY),
        (httpx.Response(408), FailureKind.AVAILABILITY),
        (httpx.Response(409, json={"name": "concurrent_idempotent_requests"}), FailureKind.AVAILABILITY),
    ],
)
async def test_send_email_retries_safe_failures(response: httpx.Response, kind: FailureKind) -> None:
    client = make_client(response)
    with pytest.raises(RetryError) as raised:
        await run_mutation(client.send_email(), send_call(), INPUT)
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
async def test_send_email_transport_failure_retries() -> None:
    def fail(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("refused", request=request)

    client = Client(
        Config(base_url="http://localhost:8848"),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(KEY))}),
        httpx.AsyncClient(transport=httpx.MockTransport(fail)),
    )
    with pytest.raises(RetryError):
        await run_mutation(client.send_email(), send_call(), INPUT)


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "response",
    [
        httpx.Response(409, json={"name": "invalid_idempotent_request"}),
        httpx.Response(422, json={"name": "validation_error"}),
        httpx.Response(401),
    ],
)
async def test_send_email_definite_rejections(response: httpx.Response) -> None:
    result = await run_mutation(make_client(response).send_email(), send_call(), INPUT)
    assert result.branch == PROVIDER_REJECTED
    assert result.failure is not None and result.failure.kind == FailureKind.PROVIDER_REJECTION


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "kind"),
    [
        (httpx.Response(200, json={}), FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"not json"), FailureKind.PROTOCOL),
        (httpx.Response(200, json={"id": "x" * 64}), FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_send_email_invalid_responses(response: httpx.Response, kind: FailureKind) -> None:
    client = make_client(response, config=Config(base_url="http://localhost:8848", max_response_bytes=32))
    result = await run_mutation(client.send_email(), send_call(), INPUT)
    assert result.branch == INVALID_RESPONSE
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
async def test_get_email_escapes_id_and_decodes_email() -> None:
    requests: list[httpx.Request] = []
    body = {
        "id": EMAIL_ID,
        "from": "Acme <hello@example.test>",
        "to": ["reader@example.test"],
        "subject": "Welcome",
        "created_at": "2026-10-26T12:00:00Z",
        "html": "<p>ignored</p>",
    }
    client = make_client(httpx.Response(200, json=body), requests)
    result = await run_query(client.get_email(), get_call(), GetEmailInput(EMAIL_ID))
    assert result.branch == FOUND
    assert result.value.to == ("reader@example.test",)
    assert result.value.created_at == "2026-10-26T12:00:00Z"
    await run_query(client.get_email(), get_call(), GetEmailInput("a/b"))
    assert requests[1].url.raw_path == b"/emails/a%2Fb"


@pytest.mark.asyncio
async def test_get_email_branches() -> None:
    missing = await run_query(
        make_client(httpx.Response(404)).get_email(), get_call(), GetEmailInput(EMAIL_ID)
    )
    assert missing.branch == NOT_FOUND
    rejected = await run_query(
        make_client(httpx.Response(403)).get_email(), get_call(), GetEmailInput(EMAIL_ID)
    )
    assert rejected.branch == PROVIDER_REJECTED
    invalid = await run_query(
        make_client(httpx.Response(200, json={"id": 7})).get_email(), get_call(), GetEmailInput(EMAIL_ID)
    )
    assert invalid.branch == INVALID_RESPONSE
    blank = await run_query(make_client(httpx.Response(200)).get_email(), get_call(), GetEmailInput(""))
    assert blank.branch == DEFECT
    with pytest.raises(RetryError):
        await run_query(make_client(httpx.Response(502)).get_email(), get_call(), GetEmailInput(EMAIL_ID))
