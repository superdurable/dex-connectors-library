# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Square payment Queries."""

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
    run_query,
)

from dex_connectors.square import (
    DEFECT,
    FOUND,
    GET_PAYMENT_DEFINITION,
    INVALID_RESPONSE,
    LIST_PAYMENTS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    SQUARE_VERSION,
    Client,
    Config,
    Credentials,
    GetPaymentInput,
    ListPaymentsInput,
    Money,
)

CONNECTION = ConnectionRef("square", "square")
TOKEN = "EAAA_test_token"
PAYMENT = {
    "id": "pay_101",
    "status": "COMPLETED",
    "amount_money": {"amount": 2900, "currency": "USD"},
    "note": "x",
}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, token: str = TOKEN
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848/v2", max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(token))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def list_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_PAYMENTS_DEFINITION.operation)


def get_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_PAYMENT_DEFINITION.operation)


@pytest.mark.asyncio
async def test_list_sends_version_token_and_cursor() -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json={"payments": [PAYMENT], "cursor": "next 1"}), requests)
    result = await run_query(client.list_payments(), list_call(), ListPaymentsInput("abc&d"))
    assert result.branch == LISTED
    assert result.value.payments[0].amount_money == Money(2900, "USD")
    assert result.value.cursor == "next 1"
    assert requests[0].url.raw_path == b"/v2/payments?cursor=abc%26d"
    assert requests[0].headers["Authorization"] == "Bearer " + TOKEN
    assert requests[0].headers["Square-Version"] == SQUARE_VERSION
    empty = await run_query(
        make_client(httpx.Response(200, json={})).list_payments(), list_call(), ListPaymentsInput()
    )
    assert empty.branch == LISTED and empty.value.payments == ()


@pytest.mark.asyncio
async def test_get_unwraps_payment_and_escapes_id() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json={"payment": PAYMENT}), requests).get_payment(),
        get_call(),
        GetPaymentInput("pay 101"),
    )
    assert result.branch == FOUND
    assert result.value.status == "COMPLETED"
    assert requests[0].url.raw_path == b"/v2/payments/pay%20101"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(404), NOT_FOUND, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (httpx.Response(400), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(200, json={}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (
            httpx.Response(200, json={"payment": {"id": "p", "amount_money": {"amount": "1"}}}),
            INVALID_RESPONSE,
            FailureKind.PROTOCOL,
        ),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_get_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(make_client(response).get_payment(), get_call(), GetPaymentInput("pay_101"))
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [408, 429, 500, 503])
async def test_safe_failures_retry(status: int) -> None:
    with pytest.raises(RetryError):
        await run_query(make_client(httpx.Response(status)).list_payments(), list_call(), ListPaymentsInput())


@pytest.mark.asyncio
async def test_409_is_a_definite_rejection() -> None:
    result = await run_query(
        make_client(httpx.Response(409)).list_payments(), list_call(), ListPaymentsInput()
    )
    assert result.branch == PROVIDER_REJECTED


@pytest.mark.asyncio
@pytest.mark.parametrize("payment_id", ["", ".", "..", "a/b", "a?b", "a\x01"])
async def test_invalid_payment_id_is_defect_without_request(payment_id: str) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests).get_payment(), get_call(), GetPaymentInput(payment_id)
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
async def test_blank_token_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests, token="").list_payments(), list_call(), ListPaymentsInput()
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHENTICATION
    assert requests == []
