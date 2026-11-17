# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay fixed-clock Go-recorded Douyin scenarios through the Python Query."""

import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.douyin as douyin

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/douyin/scenarios.json")
CONNECTION = sdk.ConnectionRef("douyin", "douyin")
CHINA_TIME = timezone(timedelta(hours=8))
FIXED_TIME = datetime(2026, 11, 16, 12, tzinfo=CHINA_TIME)
ADAPTER = conformance.PythonOperationAdapter(
    "douyin",
    CONNECTION,
    douyin,
    lambda http_client, recorder: douyin.Client(
        douyin.Config(
            max_response_bytes=8
            if recorder.scenario["name"] == "getBehaviorSummary-token-oversize"
            else 1048576,
        ),
        conformance.static_credentials(
            CONNECTION, douyin.Credentials("tt-test-app", sdk.SecretString("test-client-secret"))
        )
        if recorder.scenario["name"] != "getBehaviorSummary-missing-secret"
        else sdk.StaticCredentialProvider({}),
        http_client,
        current_time=lambda: FIXED_TIME,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact requests, outputs, branches, and retry decisions against Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
