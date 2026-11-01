# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the OpenWeatherMap Queries."""

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

from dex_connectors.openweathermap import (
    DEFECT,
    FOUND,
    GET_CURRENT_WEATHER_DEFINITION,
    GET_FORECAST_DEFINITION,
    INVALID_RESPONSE,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    WeatherInput,
)
from dex_connectors.openweathermap.client import weather_query

CONNECTION = ConnectionRef("openweathermap", "openweathermap")
KEY = "0123456789abcdef"
CURRENT = {"name": "Meridian", "main": {"temp": 21.5}, "weather": [{"description": "clear sky"}]}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, key: str = KEY
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848", max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(key))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def current_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_CURRENT_WEATHER_DEFINITION.operation)


def forecast_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GET_FORECAST_DEFINITION.operation)


@pytest.mark.asyncio
async def test_current_weather_sends_coordinates_units_and_appid() -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json=CURRENT), requests)
    result = await run_query(
        client.get_current_weather(), current_call(), WeatherInput(47.6, -122.33, "metric")
    )
    assert result.branch == FOUND
    assert (result.value.city, result.value.temperature, result.value.description) == (
        "Meridian",
        21.5,
        "clear sky",
    )
    assert requests[0].url.path == "/data/2.5/weather"
    assert dict(requests[0].url.params) == {"appid": KEY, "lat": "47.6", "lon": "-122.33", "units": "metric"}


@pytest.mark.asyncio
async def test_forecast_decodes_points() -> None:
    body = {"city": {"name": "Meridian"}, "list": [{"dt": 1793210400, "main": {"temp": 20}}]}
    result = await run_query(
        make_client(httpx.Response(200, json=body)).get_forecast(), forecast_call(), WeatherInput(1, 2)
    )
    assert result.branch == FOUND
    assert result.value.entries[0].temperature == 20.0
    assert result.value.city == "Meridian"


def test_query_formats_degrees_like_go() -> None:
    query = weather_query(WeatherInput(0.00001, 10))
    assert query == {"lat": "0.00001", "lon": "10", "units": "standard"}


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [WeatherInput(91, 0), WeatherInput(0, -181), WeatherInput(float("nan"), 0), WeatherInput(0, 0, "kelvin")],
)
async def test_invalid_input_is_defect_without_request(value: WeatherInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests).get_current_weather(), current_call(), value
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.VALIDATION
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(404), NOT_FOUND, FailureKind.NOT_FOUND),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (
            httpx.Response(200, json={"name": "x", "main": {}, "weather": [{}]}),
            INVALID_RESPONSE,
            FailureKind.PROTOCOL,
        ),
        (httpx.Response(200, json={**CURRENT, "weather": []}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_current_weather_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(make_client(response).get_current_weather(), current_call(), WeatherInput(1, 2))
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body", [{"city": {}}, {"list": [{"dt": 0, "main": {"temp": 1}}]}, {"list": [{"dt": 1}]}]
)
async def test_incomplete_forecasts_are_invalid(body: object) -> None:
    result = await run_query(
        make_client(httpx.Response(200, json=body)).get_forecast(), forecast_call(), WeatherInput(1, 2)
    )
    assert result.branch == INVALID_RESPONSE


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [408, 429, 500])
async def test_safe_failures_retry(status: int) -> None:
    with pytest.raises(RetryError):
        await run_query(
            make_client(httpx.Response(status)).get_forecast(), forecast_call(), WeatherInput(1, 2)
        )


@pytest.mark.asyncio
async def test_blank_key_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests, key="").get_forecast(), forecast_call(), WeatherInput()
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHENTICATION
    assert requests == []
