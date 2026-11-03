# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Datadog Queries."""

from uuid import uuid4

import httpx
import pytest
from dex import CodecRegistry
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

from dex_connectors.datadog import (
    DEFECT,
    INVALID_RESPONSE,
    LIST_MONITORS_DEFINITION,
    LISTED,
    PROVIDER_REJECTED,
    QUERIED,
    QUERY_METRICS_DEFINITION,
    Client,
    Config,
    Credentials,
    ListMonitorsInput,
    QueryMetricsInput,
)

CONNECTION = ConnectionRef("datadog", "datadog")
METRICS = {
    "status": "ok",
    "series": [{"metric": "system.cpu.user", "pointlist": [[1761436800000, 0.23], [1761436860000, None]]}],
}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, app_key: str = "app"
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848", max_response_bytes=400),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString("api"), SecretString(app_key))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def metrics_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, QUERY_METRICS_DEFINITION.operation)


def monitors_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_MONITORS_DEFINITION.operation)


@pytest.mark.asyncio
async def test_metrics_query_sends_interval_and_both_keys() -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json=METRICS), requests)
    result = await run_query(
        client.query_metrics(), metrics_call(), QueryMetricsInput(100, 200, "avg:system.cpu.user{*}")
    )
    assert result.branch == QUERIED
    assert result.value.series[0].pointlist == ((1761436800000, 0.23), (1761436860000, None))
    encoded = CodecRegistry().resolve(type(result.value)).encode(result.value)
    assert '"pointlist":[[1761436800000,0.23],[1761436860000,null]]' in encoded.data
    assert dict(requests[0].url.params) == {"from": "100", "query": "avg:system.cpu.user{*}", "to": "200"}
    assert (requests[0].headers["DD-API-KEY"], requests[0].headers["DD-APPLICATION-KEY"]) == ("api", "app")


@pytest.mark.asyncio
async def test_monitors_page_defaults_size() -> None:
    requests: list[httpx.Request] = []
    body = [{"id": 1, "name": "latency", "type": "metric alert", "overall_state": "OK"}]
    client = make_client(httpx.Response(200, json=body), requests)
    result = await run_query(client.list_monitors(), monitors_call(), ListMonitorsInput())
    assert result.branch == LISTED
    assert result.value.monitors[0].overall_state == "OK"
    assert requests[0].url.raw_path == b"/api/v1/monitor?page=0&page_size=100"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [
        {"status": "error", "series": []},
        {"status": "ok"},
        {"status": "ok", "series": [{"pointlist": [["x"]]}]},
    ],
)
async def test_unsuccessful_metric_bodies_are_invalid(body: object) -> None:
    result = await run_query(
        make_client(httpx.Response(200, json=body)).query_metrics(),
        metrics_call(),
        QueryMetricsInput(1, 2, "q"),
    )
    assert result.branch == INVALID_RESPONSE


@pytest.mark.asyncio
async def test_inexact_metric_number_names_field_and_fails_terminally() -> None:
    body = {"status": "ok", "series": [{"metric": "cpu", "pointlist": [[9007199254740993, 0.23]]}]}
    result = await run_query(
        make_client(httpx.Response(200, json=body)).query_metrics(),
        metrics_call(),
        QueryMetricsInput(1, 2, "q"),
    )
    assert result.branch == INVALID_RESPONSE
    assert result.failure is not None
    assert "series[0].pointlist[0][0]" in result.failure.message


@pytest.mark.asyncio
async def test_integral_metric_float_uses_go_json_number_form() -> None:
    body = {"status": "ok", "series": [{"metric": "cpu", "pointlist": [[1000.0, 1.0]]}]}
    result = await run_query(
        make_client(httpx.Response(200, json=body)).query_metrics(),
        metrics_call(),
        QueryMetricsInput(1, 2, "q"),
    )
    encoded = CodecRegistry().resolve(type(result.value)).encode(result.value)
    assert '"pointlist":[[1000,1]]' in encoded.data


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "kind"),
    [
        (401, FailureKind.AUTHENTICATION),
        (403, FailureKind.AUTHORIZATION),
        (404, FailureKind.NOT_FOUND),
        (400, FailureKind.VALIDATION),
    ],
)
async def test_rejections(status: int, kind: FailureKind) -> None:
    result = await run_query(
        make_client(httpx.Response(status)).list_monitors(), monitors_call(), ListMonitorsInput()
    )
    assert result.branch == PROVIDER_REJECTED
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [408, 409, 429, 500])
async def test_safe_failures_retry(status: int) -> None:
    with pytest.raises(RetryError):
        await run_query(
            make_client(httpx.Response(status)).list_monitors(), monitors_call(), ListMonitorsInput()
        )


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("operation", "value"),
    [
        ("metrics", QueryMetricsInput(0, 1, "q")),
        ("metrics", QueryMetricsInput(2, 2, "q")),
        ("metrics", QueryMetricsInput(1, 2, " ")),
        ("monitors", ListMonitorsInput(page=-1)),
        ("monitors", ListMonitorsInput(page_size=1001)),
    ],
)
async def test_invalid_input_is_defect_without_request(operation: str, value: object) -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200), requests)
    if operation == "metrics":
        result = await run_query(client.query_metrics(), metrics_call(), value)  # type: ignore[arg-type]
    else:
        result = await run_query(client.list_monitors(), monitors_call(), value)  # type: ignore[arg-type]
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
async def test_missing_application_key_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200), requests, app_key="").list_monitors(),
        monitors_call(),
        ListMonitorsInput(),
    )
    assert result.branch == DEFECT
    assert requests == []
