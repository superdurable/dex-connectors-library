# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Intercom provider scenarios."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.intercom as intercom

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/intercom/scenarios.json")
CONNECTION = sdk.ConnectionRef("intercom", "intercom-test")
ADAPTER = PythonOperationAdapter(
    "intercom",
    CONNECTION,
    intercom,
    lambda transport, recorder: intercom.Client(
        intercom.Config(),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: intercom.Credentials(
                    sdk.SecretString("dG9rOjBiMjY3intercomTestToken0123456789"), sdk.SecretString("")
                )
            }
        ),
        transport,
        now=lambda: datetime.fromisoformat(
            recorder.scenario.get("fixedClock", "2026-10-30T00:00:00Z")
        ).astimezone(UTC),
        endpoint="http://127.0.0.1:37345",
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check provider exchanges, output, retries, and heartbeat writes against Go."""
    await replay(scenario, ADAPTER.invoke)
