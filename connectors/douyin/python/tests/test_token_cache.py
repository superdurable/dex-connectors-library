# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify expired-token recovery and the China 10:00 report cutoff."""

import json
from datetime import datetime, timedelta, timezone

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.douyin import FOUND, Client, Config, Credentials, GetBehaviorSummaryInput
from dex_connectors.douyin.client import _report_day

CHINA_TIME = timezone(timedelta(hours=8))
FIXED_TIME = datetime(2026, 11, 16, 12, tzinfo=CHINA_TIME)


def test_report_cutoff_uses_china_ten_am() -> None:
    """Keep yesterday unavailable until the documented update deadline."""
    before = datetime(2026, 11, 16, 9, 59, tzinfo=CHINA_TIME)
    after = before + timedelta(minutes=1)
    assert _report_day("2026-11-15", before) is None
    assert _report_day("2026-11-15", after) is not None
    assert _report_day("2026-11-16", after) is None
    assert _report_day("2026-08-17", after) is None


@pytest.mark.asyncio
async def test_expired_token_is_reacquired_without_secret_leak() -> None:
    """An expired report token is discarded before the next Query attempt."""
    token_requests = 0
    report_requests = 0

    def answer(request: httpx.Request) -> httpx.Response:
        nonlocal token_requests, report_requests
        if request.url.path == "/oauth/client_token/":
            token_requests += 1
            assert json.loads(request.content)["client_secret"] == "test-client-secret"
            return httpx.Response(
                200,
                json={
                    "data": {"error_code": 0, "access_token": f"token-{token_requests}", "expires_in": 7200},
                },
            )
        report_requests += 1
        assert request.headers["access-token"] == f"token-{report_requests}"
        if report_requests == 1:
            return httpx.Response(200, json={"err_no": 28001008, "err_msg": "test-client-secret"})
        return httpx.Response(
            200,
            json={
                "err_no": 0,
                "data": {
                    "sum": {
                        "active_user_num": 1,
                        "new_user_num": 2,
                        "total_user_num": 3,
                        "open_time": 4,
                        "share_time": 5,
                    }
                },
            },
        )

    reference = ConnectionRef("douyin", "douyin")
    credentials = Credentials("tt-test-app", SecretString("test-client-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(
            Config(),
            StaticCredentialProvider({reference: credentials}),
            http_client,
            current_time=lambda: FIXED_TIME,
        )
        call = Call(CallID("token-cache"), reference, OperationRef("douyin", "getBehaviorSummary"))
        first = await client.get_behavior_summary().invoke(call, GetBehaviorSummaryInput("2026-11-14"))
        second = await client.get_behavior_summary().invoke(call, GetBehaviorSummaryInput("2026-11-14"))
    assert first.kind == "retry" and first.failure is not None
    assert "test-client-secret" not in first.failure.message
    assert second.branch == FOUND
    assert token_requests == 2 and report_requests == 2
