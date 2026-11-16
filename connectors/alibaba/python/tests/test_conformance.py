# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Alibaba Cloud ECS provider scenarios."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.alibaba as alibaba

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/alibaba/scenarios.json")
CONNECTION = sdk.ConnectionRef("alibaba", "alibaba")
_NONCE = bytes.fromhex("3156853299f313e23dc12e1703dc12e1")
_DATE = datetime(2023, 10, 26, 10, 22, 32, tzinfo=UTC)
ADAPTER = conformance.PythonOperationAdapter(
    "alibaba",
    CONNECTION,
    alibaba,
    lambda http_client, recorder: alibaba.Client(
        alibaba.Config(
            base_url="http://127.0.0.1:12345",
            max_response_bytes=4 if recorder.scenario["name"] == "describeRegions-oversized" else 1048576,
        ),
        conformance.static_credentials(
            CONNECTION,
            alibaba.Credentials(
                sdk.SecretString(
                    "bad key"
                    if recorder.scenario["name"] == "describeRegions-defect"
                    else "test-access-key-id"
                ),
                sdk.SecretString("test-access-key-secret"),
            ),
        ),
        http_client,
        clock=lambda: _DATE,
        nonce_source=lambda length: _NONCE,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare signed requests and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
