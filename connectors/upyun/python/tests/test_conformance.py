# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded UPYUN provider scenarios."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.upyun as upyun

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/upyun/scenarios.json")
CONNECTION = sdk.ConnectionRef("upyun", "upyun")
ADAPTER = conformance.PythonOperationAdapter(
    "upyun",
    CONNECTION,
    upyun,
    lambda http_client, recorder: upyun.Client(
        upyun.Config(
            bucket="bucket",
            max_response_bytes=3 if recorder.scenario["name"] == "getServiceUsage-oversized" else 1024,
        ),
        conformance.static_credentials(CONNECTION, upyun.Credentials("upyun", sdk.SecretString("password")))
        if recorder.scenario["name"] != "getServiceUsage-defect"
        else sdk.StaticCredentialProvider({}),
        http_client,
        lambda: datetime(2025, 1, 2, 3, 4, 5, tzinfo=UTC),
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact Go requests, branches, outputs, and retry decisions."""
    await conformance.replay(scenario, ADAPTER.invoke)
