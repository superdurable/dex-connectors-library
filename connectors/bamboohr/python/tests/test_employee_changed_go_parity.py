# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go employeeChanged fetcher's recorded outputs and require identical cursors and event IDs.

The fixture was recorded from the Go connector's employeeChangeFetcher: each step serves one raw
changed-employees response, and each step's cursor token must match Go byte for byte, so Go and Python
replicas share one poll cursor document.
"""

import json
from datetime import datetime
from pathlib import Path
from typing import Any, cast
from urllib.parse import parse_qsl

import httpx
import pytest
from dex import CodecRegistry
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
)
from dex_connectors.sdk.pollingtrigger import Cursor, CursorInvalidError, RateLimitedError

from dex_connectors.bamboohr import (
    Client,
    Config,
    Credentials,
    EmployeeChangedEvent,
    EmployeeChangedTriggerConfiguration,
)
from dex_connectors.bamboohr._employee_change_fetcher import EmployeeChangeFetcher

FIXTURE = json.loads(
    (Path(__file__).parent / "fixtures" / "employee-changed-go-fetcher-cases.json").read_text()
)
REFERENCE = ConnectionRef("bamboohr", "bamboohr-trigger-test")
# Split so push protection does not read a fixture key as a credential.
API_KEY = "parity" + "-key"
LOCAL_NOW = datetime.fromisoformat(FIXTURE["localNow"])


def _fetcher(step: dict[str, Any], requests: list[httpx.Request]) -> EmployeeChangeFetcher:
    response = step["response"]

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        headers = {"Content-Type": "application/json"}
        if response.get("date"):
            headers["Date"] = response["date"]
        if response.get("retryAfter"):
            headers["Retry-After"] = response["retryAfter"]
        return httpx.Response(response["status"], headers=headers, content=response["body"].encode())

    client = Client(
        Config(company_domain="acme"),
        StaticCredentialProvider({REFERENCE: Credentials(SecretString(API_KEY))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
        "https://acme.example/api/v1",
        clock=lambda: LOCAL_NOW,
    )
    configuration = EmployeeChangedTriggerConfiguration(change_type=step.get("changeType") or None)
    return EmployeeChangeFetcher(client, REFERENCE, configuration)


def _payload_json(event: EmployeeChangedEvent) -> Any:
    return json.loads(cast(str, CodecRegistry().resolve(EmployeeChangedEvent).encode(event).data))


def _go_message(error: BaseException) -> str:
    return str(error)


@pytest.mark.parametrize("step", FIXTURE["steps"], ids=[step["name"] for step in FIXTURE["steps"]])
async def test_python_fetcher_matches_the_go_recording(step: dict[str, Any]) -> None:
    requests: list[httpx.Request] = []
    fetcher = _fetcher(step, requests)
    context = TriggerContext(TriggerCancellation())
    try:
        if step.get("seed"):
            cursor = await fetcher.seed_cursor(context)
            page = None
        else:
            page = await fetcher.fetch_changes(context, Cursor(token=step["cursor"]))
            cursor = page.next_cursor
    except Exception as error:
        assert step.get("error"), f"Go succeeded, Python raised {error!r}"
        kind = (
            "cursorInvalid"
            if isinstance(error, CursorInvalidError)
            else "rateLimited"
            if isinstance(error, RateLimitedError)
            else "failed"
        )
        assert kind == step["errorKind"]
        assert _go_message(error) == step["error"]
        if isinstance(error, RateLimitedError):
            assert error.retry_after.total_seconds() == step.get("retryAfterSeconds", 0)
        assert len(requests) == step["requests"]
        return
    assert not step.get("error"), f"Go raised {step['error']}, Python succeeded"
    assert cursor.token == step["next"]
    assert len(requests) == step["requests"]
    if requests:
        assert parse_qsl(requests[0].url.query.decode()) == parse_qsl(step["query"])
    if page is None:
        return
    assert page.has_more_pages == step.get("hasMore", False)
    assert len(page.changes) == step["eventCount"]
    recorded = step["events"] or []
    delivered = list(page.changes)
    if step.get("eventsAbridged"):
        delivered = delivered[:3] + delivered[-2:]
    assert [event.id for event in delivered] == [event["id"] for event in recorded]
    for event, expected in zip(delivered, recorded, strict=True):
        assert event.occurred_at == datetime.fromisoformat(expected["occurredAt"])
        assert _payload_json(event.payload) == expected["payload"]
