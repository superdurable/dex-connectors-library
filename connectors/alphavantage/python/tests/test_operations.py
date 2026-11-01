# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Alpha Vantage Queries."""

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

from dex_connectors.alphavantage import (
    DEFECT,
    FOUND,
    GET_GLOBAL_QUOTE_DEFINITION,
    GET_TIME_SERIES_DAILY_DEFINITION,
    INVALID_RESPONSE,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    StockInput,
)

CONNECTION = ConnectionRef("alphavantage", "alphavantage")
KEY = "demo_key"
QUOTE = {"Global Quote": {"01. symbol": "IBM", "05. price": "255.25", "07. latest trading day": "2026-10-28"}}
DAY = {"1. open": "1", "2. high": "2", "3. low": "0.5", "4. close": "1.5", "5. volume": "10"}


def make_client(response: httpx.Response, requests: list[httpx.Request] | None = None) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848", max_response_bytes=400),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(KEY))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def quote_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_GLOBAL_QUOTE_DEFINITION.operation)


def daily_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_TIME_SERIES_DAILY_DEFINITION.operation)


@pytest.mark.asyncio
async def test_quote_sends_function_symbol_and_apikey() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=QUOTE), requests).get_global_quote(),
        quote_call(),
        StockInput("IBM"),
    )
    assert result.branch == FOUND
    assert result.value.latest_trading_day == "2026-10-28"
    assert dict(requests[0].url.params) == {"apikey": KEY, "function": "GLOBAL_QUOTE", "symbol": "IBM"}
    assert requests[0].url.path == "/query"


@pytest.mark.asyncio
async def test_daily_series_is_sorted_newest_first() -> None:
    body = {"Time Series (Daily)": {"2026-10-27": DAY, "2026-10-28": DAY}}
    result = await run_query(
        make_client(httpx.Response(200, json=body)).get_time_series_daily(), daily_call(), StockInput("IBM")
    )
    assert result.branch == FOUND
    assert [day.date for day in result.value.days] == ["2026-10-28", "2026-10-27"]
    assert result.value.symbol == "IBM"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("body", "branch"),
    [
        ({"Global Quote": {}}, NOT_FOUND),
        ({}, NOT_FOUND),
        ({"Global Quote": {"01. symbol": "IBM"}}, INVALID_RESPONSE),
        ({"Information": "Invalid API call."}, PROVIDER_REJECTED),
        ({"Error Message": "Invalid API call."}, PROVIDER_REJECTED),
        ([1], INVALID_RESPONSE),
    ],
)
async def test_quote_body_classification(body: object, branch: str) -> None:
    result = await run_query(
        make_client(httpx.Response(200, json=body)).get_global_quote(), quote_call(), StockInput("IBM")
    )
    assert result.branch == branch


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [
        {"Note": "Thank you for using Alpha Vantage!"},
        {"Information": "You have reached the 25 requests per day limit."},
    ],
)
async def test_rate_limit_notices_retry(body: object) -> None:
    with pytest.raises(RetryError) as raised:
        await run_query(
            make_client(httpx.Response(200, json=body)).get_global_quote(), quote_call(), StockInput("IBM")
        )
    assert raised.value.failure.kind == FailureKind.RATE_LIMIT


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch"),
    [
        (httpx.Response(200, json={"Time Series (Daily)": {}}), NOT_FOUND),
        (
            httpx.Response(200, json={"Time Series (Daily)": {"2026-10-28": {"1. open": "1"}}}),
            INVALID_RESPONSE,
        ),
        (httpx.Response(404), NOT_FOUND),
        (httpx.Response(403), PROVIDER_REJECTED),
    ],
)
async def test_daily_branches(response: httpx.Response, branch: str) -> None:
    result = await run_query(make_client(response).get_time_series_daily(), daily_call(), StockInput("IBM"))
    assert result.branch == branch


@pytest.mark.asyncio
@pytest.mark.parametrize("symbol", ["", ".IBM", "IBM/X", "A" * 26])
async def test_invalid_symbol_is_defect_without_request(symbol: str) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests).get_global_quote(), quote_call(), StockInput(symbol)
    )
    assert result.branch == DEFECT
    assert requests == []
