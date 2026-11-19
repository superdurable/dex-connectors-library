# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded PingCAP requests and decisions."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.pingcap as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/pingcap/scenarios.json")
CONNECTION = sdk.ConnectionRef("pingcap", "pingcap")


def make_client(http_client: object, recorder: object) -> connector.Client:
    """Use Go recording's clock and synthetic credentials."""
    scenario = recorder.scenario  # type: ignore[attr-defined]
    defect = scenario["name"].endswith("defect")
    return connector.Client(
        connector.Config(
            max_response_bytes=5
            if scenario["name"].endswith("oversized")
            else 16
            if scenario["name"].endswith("oversized")
            else 1048576
        ),
        conformance.static_credentials(
            CONNECTION,
            connector.Credentials(
                sdk.SecretString("" if defect else "test-public-key"), sdk.SecretString("test-private-key")
            ),
        ),
        http_client,  # type: ignore[arg-type]
        nonce_source=conformance.SyntheticRandomSource(
            scenario.get("randomSeed", "SYNTHETIC_CONFORMANCE_PINGCAP_defect")
        ).read,
    )


ADAPTER = conformance.PythonOperationAdapter("pingcap", CONNECTION, connector, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare full requests, outputs and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
