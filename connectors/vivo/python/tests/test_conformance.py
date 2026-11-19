# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded vivo provider scenarios."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.vivo as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/vivo/scenarios.json")
CONNECTION = sdk.ConnectionRef("vivo", "vivo")


def make_client(http_client: httpx.AsyncClient, recorder: object) -> connector.Client:
    """Supply the fixture's inert credentials and deterministic signed timestamp."""
    credentials = connector.Credentials(sdk.SecretString("test-account"), sdk.SecretString("test-secret"))
    return connector.Client(
        connector.Config(),
        conformance.static_credentials(CONNECTION, credentials),
        http_client,
        lambda: datetime.fromtimestamp(1731510000, UTC),
    )


ADAPTER = conformance.PythonOperationAdapter("vivo", CONNECTION, connector, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact Go requests, outputs, and retry decisions."""
    await conformance.replay(scenario, ADAPTER.invoke)
