# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the PayPal operations."""

import base64
from collections.abc import Callable
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

from dex_connectors.paypal import (
    DEFECT,
    FOUND,
    GET_ORDER_DEFINITION,
    INVALID_RESPONSE,
    LIST_TRANSACTIONS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    GetOrderInput,
    ListTransactionsInput,
    Order,
)

CONNECTION = ConnectionRef("paypal", "paypal")
TOKEN = {"access_token": "A21AA-token", "token_type": "Bearer", "expires_in": 32400}
ORDER = {"id": "ORDER101", "status": "COMPLETED", "intent": "CAPTURE"}
WINDOW = ListTransactionsInput("2026-10-01T00:00:00Z", "2026-10-31T00:00:00-07:00", 2)


def make_client(
    respond_api: Callable[[httpx.Request], httpx.Response],
    requests: list[httpx.Request],
    token_response: httpx.Response | None = None,
    client_secret: str = "secret",
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path == "/v1/oauth2/token":
            return token_response or httpx.Response(200, json=TOKEN)
        return respond_api(request)

    return Client(
        Config("http://localhost:8848", max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials("client-id", SecretString(client_secret))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def order_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_ORDER_DEFINITION.operation)


def transactions_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_TRANSACTIONS_DEFINITION.operation)


def respond_with(response: httpx.Response) -> Callable[[httpx.Request], httpx.Response]:
    return lambda request: response


@pytest.mark.asyncio
async def test_get_order_exchanges_and_caches_client_credentials_token() -> None:
    requests: list[httpx.Request] = []
    client = make_client(respond_with(httpx.Response(200, json=ORDER)), requests)
    first = await run_query(client.get_order(), order_call(), GetOrderInput("ORDER101"))
    second = await run_query(client.get_order(), order_call(), GetOrderInput("ORDER101"))
    assert first.branch == FOUND and second.branch == FOUND
    assert first.value == Order("ORDER101", "COMPLETED")
    token_request, read_request, cached_read = requests
    assert token_request.method == "POST"
    assert token_request.content == b"grant_type=client_credentials"
    assert token_request.headers["Authorization"] == "Basic " + base64.b64encode(b"client-id:secret").decode()
    assert token_request.headers["Content-Type"] == "application/x-www-form-urlencoded"
    assert str(read_request.url) == "http://localhost:8848/v2/checkout/orders/ORDER101"
    assert read_request.headers["Authorization"] == "Bearer A21AA-token"
    assert cached_read.headers["Authorization"] == "Bearer A21AA-token"


@pytest.mark.asyncio
async def test_short_lived_token_is_not_cached_and_401_clears_cache() -> None:
    requests: list[httpx.Request] = []
    short = httpx.Response(200, json={**TOKEN, "expires_in": 30})
    client = make_client(respond_with(httpx.Response(200, json=ORDER)), requests, short)
    await run_query(client.get_order(), order_call(), GetOrderInput("ORDER101"))
    await run_query(client.get_order(), order_call(), GetOrderInput("ORDER101"))
    assert [request.url.path for request in requests].count("/v1/oauth2/token") == 2
    requests.clear()
    rejecting = make_client(respond_with(httpx.Response(401)), requests)
    rejected = await run_query(rejecting.get_order(), order_call(), GetOrderInput("ORDER101"))
    assert rejected.branch == PROVIDER_REJECTED
    assert rejected.failure is not None and rejected.failure.kind == FailureKind.AUTHENTICATION
    await run_query(rejecting.get_order(), order_call(), GetOrderInput("ORDER101"))
    assert [request.url.path for request in requests].count("/v1/oauth2/token") == 2


@pytest.mark.asyncio
async def test_list_transactions_sends_window_and_decodes_page() -> None:
    requests: list[httpx.Request] = []
    body = {
        "transaction_details": [
            {
                "transaction_info": {
                    "transaction_id": "TXN101",
                    "transaction_status": "S",
                    "transaction_amount": None,
                }
            },
            None,
        ],
        "page": 2,
        "total_pages": 3,
    }
    result = await run_query(
        make_client(respond_with(httpx.Response(200, json=body)), requests).list_transactions(),
        transactions_call(),
        WINDOW,
    )
    assert result.branch == LISTED
    assert result.value.page == 2 and result.value.total_pages == 3
    assert result.value.transaction_details[0].transaction_info.transaction_id == "TXN101"
    assert result.value.transaction_details[1].transaction_info.transaction_id == ""
    assert dict(requests[1].url.params) == {
        "end_date": "2026-10-31T00:00:00-07:00",
        "fields": "transaction_info",
        "page": "2",
        "start_date": "2026-10-01T00:00:00Z",
    }


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [
        ListTransactionsInput("2026-10-01", "2026-10-02T00:00:00Z"),
        ListTransactionsInput("2026-10-01T00:00:00", "2026-10-02T00:00:00Z"),
        ListTransactionsInput("2026-10-02T00:00:00Z", "2026-10-01T00:00:00Z"),
        ListTransactionsInput("2026-10-01T00:00:00Z", "2026-11-02T00:00:01Z"),
        ListTransactionsInput("2026-10-01T00:00:00Z", "2026-10-02T00:00:00Z", -1),
    ],
)
async def test_invalid_window_is_defect_without_request(value: ListTransactionsInput) -> None:
    requests: list[httpx.Request] = []
    client = make_client(respond_with(httpx.Response(200, json={})), requests)
    result = await run_query(client.list_transactions(), transactions_call(), value)
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize("order_id", ["", ".", "..", "a/b", "a?b", "a\nb"])
async def test_invalid_order_id_is_defect_without_request(order_id: str) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(respond_with(httpx.Response(200, json=ORDER)), requests).get_order(),
        order_call(),
        GetOrderInput(order_id),
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("token_response", "branch", "kind"),
    [
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(200, json={**TOKEN, "token_type": "MAC"}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, json={**TOKEN, "access_token": ""}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 70_000), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_token_exchange_failures(
    token_response: httpx.Response, branch: str, kind: FailureKind
) -> None:
    requests: list[httpx.Request] = []
    client = make_client(respond_with(httpx.Response(200, json=ORDER)), requests, token_response)
    result = await run_query(client.get_order(), order_call(), GetOrderInput("ORDER101"))
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind
    assert len(requests) == 1


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [408, 429, 503])
async def test_token_exchange_retries(status: int) -> None:
    requests: list[httpx.Request] = []
    client = make_client(respond_with(httpx.Response(200, json=ORDER)), requests, httpx.Response(status))
    with pytest.raises(RetryError):
        await run_query(client.get_order(), order_call(), GetOrderInput("ORDER101"))


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(404), NOT_FOUND, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (httpx.Response(422), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(200, json={"status": "COMPLETED"}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_read_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(respond_with(response), requests).get_order(), order_call(), GetOrderInput("ORDER101")
    )
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "kind"),
    [(408, FailureKind.AVAILABILITY), (429, FailureKind.RATE_LIMIT), (500, FailureKind.AVAILABILITY)],
)
async def test_read_retries(status: int, kind: FailureKind) -> None:
    requests: list[httpx.Request] = []
    with pytest.raises(RetryError) as raised:
        await run_query(
            make_client(respond_with(httpx.Response(status)), requests).get_order(),
            order_call(),
            GetOrderInput("ORDER101"),
        )
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
async def test_blank_secret_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(respond_with(httpx.Response(200, json=ORDER)), requests, client_secret="").get_order(),
        order_call(),
        GetOrderInput("ORDER101"),
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHENTICATION
    assert requests == []
