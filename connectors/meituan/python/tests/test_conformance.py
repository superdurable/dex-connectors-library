# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Meituan signed requests and decisions."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.meituan as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/meituan/scenarios.json")
CONNECTION = sdk.ConnectionRef("meituan", "meituan")


def make_client(http_client: object, recorder: object) -> connector.Client:
    """Use the Go recording's fixed clock for exact SHA-1 form-signature replay."""
    scenario = recorder.scenario  # type: ignore[attr-defined]
    return connector.Client(
        connector.Config(
            max_response_bytes=5 if scenario["name"] == "queryOrderStatus-oversized" else 262144
        ),
        conformance.static_credentials(
            CONNECTION,
            connector.Credentials("test-app-key", sdk.SecretString("private-secret")),
        ),
        http_client,  # type: ignore[arg-type]
        clock=lambda: datetime(2026, 1, 1, tzinfo=UTC),
    )


ADAPTER = conformance.PythonOperationAdapter("meituan", CONNECTION, connector, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare complete provider requests and SDK decisions with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
