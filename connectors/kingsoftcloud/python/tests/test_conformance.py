# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Kingsoft Cloud IAM provider scenarios."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.kingsoftcloud as kingsoftcloud

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/kingsoftcloud/scenarios.json")
CONNECTION = sdk.ConnectionRef("kingsoftcloud", "kingsoftcloud")
ADAPTER = conformance.PythonOperationAdapter(
    "kingsoftcloud",
    CONNECTION,
    kingsoftcloud,
    lambda http_client, recorder: kingsoftcloud.Client(
        kingsoftcloud.Config(
            max_response_bytes=16 if recorder.scenario["name"] == "listUsers-oversized" else 262144
        ),
        conformance.static_credentials(
            CONNECTION,
            kingsoftcloud.Credentials(
                sdk.SecretString("test-access-key-id"), sdk.SecretString("test-secret-access-key")
            ),
        ),
        http_client,
        lambda: datetime(2021, 8, 12, 2, 47, 36, tzinfo=UTC),
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact Go requests, branches, outputs, and retry decisions."""
    await conformance.replay(scenario, ADAPTER.invoke)
