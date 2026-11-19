# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded ZEGO signed requests and decisions."""

import sys
from datetime import datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.zego as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/zego/scenarios.json")
CONNECTION = sdk.ConnectionRef("zego", "zego")


def make_client(http_client: object, recorder: object) -> connector.Client:
    """Use the Go recording's fixed clock and nonce for exact MD5 replay."""
    scenario = recorder.scenario  # type: ignore[attr-defined]
    random_source = conformance.SyntheticRandomSource(scenario["randomSeed"])
    return connector.Client(
        connector.Config(
            max_response_bytes=16 if scenario["name"] == "getRoomUserCount-oversized" else 1048576
        ),
        conformance.static_credentials(
            CONNECTION,
            connector.Credentials("12345", sdk.SecretString("test-secret")),
        ),
        http_client,  # type: ignore[arg-type]
        clock=lambda: datetime.fromisoformat(scenario["fixedClock"].replace("Z", "+00:00")),
        nonce_source=random_source.read,
    )


ADAPTER = conformance.PythonOperationAdapter("zego", CONNECTION, connector, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare complete provider requests and SDK decisions with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
