# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded pgyer provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.pgyer as provider

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/pgyer/scenarios.json")
CONNECTION = sdk.ConnectionRef("pgyer", "pgyer")


def make_client(client: object, recorder: conformance.ExchangeRecorder) -> provider.Client:
    """Build the provider client from synthetic Go scenario settings."""
    key = "secretKey"
    return provider.Client(
        provider.Config(),
        conformance.static_credentials(CONNECTION, provider.Credentials(sdk.SecretString(key))),
        client,
    )


ADAPTER = conformance.PythonOperationAdapter("pgyer", CONNECTION, provider, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare provider requests and SDK results with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
