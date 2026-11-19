# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded SF Express signed route requests and decisions."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.sfexpress as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/sfexpress/scenarios.json")
CONNECTION = sdk.ConnectionRef("sfexpress", "sfexpress")


def make_client(http_client: object, recorder: object) -> connector.Client:
    """Use Go's fixed timestamp, synthetic random source, and test credentials."""
    scenario = recorder.scenario  # type: ignore[attr-defined]
    credentials = connector.Credentials("customer-1", sdk.SecretString("private checkword"))
    if scenario["name"] == "queryRoutes-defect":
        credentials = connector.Credentials("", sdk.SecretString(""))
    random_source = conformance.SyntheticRandomSource(
        scenario.get("randomSeed", "SYNTHETIC_CONFORMANCE_sfexpress_defect")
    )
    return connector.Client(
        connector.Config(
            base_url="http://127.0.0.1:1234",
            max_response_bytes=16 if scenario["name"] == "queryRoutes-oversized" else 1048576,
        ),
        conformance.static_credentials(CONNECTION, credentials),
        http_client,  # type: ignore[arg-type]
        clock=lambda: datetime.fromtimestamp(1731510000, UTC),
        request_id_source=random_source.read,
    )


ADAPTER = conformance.PythonOperationAdapter("sfexpress", CONNECTION, connector, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare complete signed requests and SDK decisions with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
