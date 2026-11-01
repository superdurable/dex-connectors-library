# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Shopify order Queries."""

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

from dex_connectors.shopify import (
    DEFECT,
    FOUND,
    GET_ORDER_DEFINITION,
    INVALID_RESPONSE,
    LIST_ORDERS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    GetOrderInput,
    ListOrdersInput,
)

CONNECTION = ConnectionRef("shopify", "shopify")
TOKEN = "shpat_test"
ORDER = {"id": 101, "name": "#101", "financial_status": "paid", "total_price": "29.00", "currency": "USD"}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, token: str = TOKEN
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(shop_subdomain="meridian-shop", max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(token))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def list_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_ORDERS_DEFINITION.operation)


def get_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_ORDER_DEFINITION.operation)


@pytest.mark.asyncio
async def test_get_reads_order_json_with_access_token() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json={"order": ORDER}), requests).get_order(),
        get_call(),
        GetOrderInput(101),
    )
    assert result.branch == FOUND
    assert result.value.financial_status == "paid"
    assert str(requests[0].url) == "https://meridian-shop.myshopify.com/admin/api/2026-10/orders/101.json"
    assert requests[0].headers["X-Shopify-Access-Token"] == TOKEN


@pytest.mark.asyncio
async def test_list_asks_for_any_status_and_limit() -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json={"orders": [ORDER]}), requests)
    result = await run_query(client.list_orders(), list_call(), ListOrdersInput(limit=5))
    assert result.branch == LISTED
    assert result.value.orders[0].name == "#101"
    assert requests[0].url.raw_path == b"/admin/api/2026-10/orders.json?limit=5&status=any"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(404), NOT_FOUND, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (httpx.Response(200, json={"order": {"id": 0}}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, json=ORDER), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_get_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(make_client(response).get_order(), get_call(), GetOrderInput(101))
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
async def test_list_requires_orders_and_retries_safe_failures() -> None:
    invalid = await run_query(
        make_client(httpx.Response(200, json={})).list_orders(), list_call(), ListOrdersInput()
    )
    assert invalid.branch == INVALID_RESPONSE
    for status in (408, 429, 502):
        with pytest.raises(RetryError):
            await run_query(make_client(httpx.Response(status)).list_orders(), list_call(), ListOrdersInput())


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("operation", "value"),
    [("list", ListOrdersInput(limit=251)), ("list", ListOrdersInput(limit=-1)), ("get", GetOrderInput(0))],
)
async def test_invalid_input_is_defect_without_request(operation: str, value: object) -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200), requests)
    if operation == "list":
        result = await run_query(client.list_orders(), list_call(), value)  # type: ignore[arg-type]
    else:
        result = await run_query(client.get_order(), get_call(), value)  # type: ignore[arg-type]
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
async def test_blank_token_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests, token="").get_order(), get_call(), GetOrderInput(1)
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.parametrize("subdomain", ["", "-shop", "shop-", "Shop", "shop.example", "a" * 64])
def test_shop_subdomain_is_validated(subdomain: str) -> None:
    with pytest.raises(ValueError):
        Client(Config(shop_subdomain=subdomain), StaticCredentialProvider({}))
