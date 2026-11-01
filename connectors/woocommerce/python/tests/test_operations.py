# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the WooCommerce order Queries."""

import base64
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

from dex_connectors.woocommerce import (
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

CONNECTION = ConnectionRef("woocommerce", "woocommerce")
ORDER = {"id": 101, "status": "processing", "total": "29.00", "currency": "USD", "line_items": []}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, secret: str = "cs_test"
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(store_url="http://localhost:8848/shop", max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString("ck_test"), SecretString(secret))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def list_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_ORDERS_DEFINITION.operation)


def get_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_ORDER_DEFINITION.operation)


@pytest.mark.asyncio
async def test_list_uses_store_api_path_and_basic_auth() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=[ORDER]), requests).list_orders(),
        list_call(),
        ListOrdersInput(page=2, per_page=10),
    )
    assert result.branch == LISTED
    assert result.value.orders[0].total == "29.00"
    assert requests[0].url.raw_path == b"/shop/wp-json/wc/v3/orders?page=2&per_page=10"
    assert requests[0].headers["Authorization"] == "Basic " + base64.b64encode(b"ck_test:cs_test").decode()


@pytest.mark.asyncio
async def test_get_reads_order_by_id() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=ORDER), requests).get_order(), get_call(), GetOrderInput(101)
    )
    assert result.branch == FOUND
    assert result.value.status == "processing"
    assert requests[0].url.raw_path == b"/shop/wp-json/wc/v3/orders/101"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(404), NOT_FOUND, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (httpx.Response(200, json={"id": 0}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, json={"id": "101"}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_get_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(make_client(response).get_order(), get_call(), GetOrderInput(101))
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
async def test_list_requires_an_array_and_retries_safe_failures() -> None:
    invalid = await run_query(
        make_client(httpx.Response(200, json={"orders": []})).list_orders(), list_call(), ListOrdersInput()
    )
    assert invalid.branch == INVALID_RESPONSE
    for status in (408, 429, 500):
        with pytest.raises(RetryError):
            await run_query(make_client(httpx.Response(status)).list_orders(), list_call(), ListOrdersInput())


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("operation", "value"),
    [("list", ListOrdersInput(page=-1)), ("list", ListOrdersInput(per_page=101)), ("get", GetOrderInput(0))],
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
async def test_blank_secret_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests, secret="").list_orders(), list_call(), ListOrdersInput()
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.parametrize("store_url", ["", "https://shop.example/wp-json/wc/v3", "/relative"])
def test_store_url_must_be_the_site_address(store_url: str) -> None:
    with pytest.raises(ValueError):
        Client(Config(store_url=store_url), StaticCredentialProvider({}))
