# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Shoplazza provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.shoplazza as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/shoplazza/scenarios.json")
CONNECTION = sdk.ConnectionRef("shoplazza", "shoplazza")


def make_client(client: object, recorder: conformance.ExchangeRecorder) -> connector.Client:
    """Build the Query using synthetic recorded credentials."""
    name = recorder.scenario["name"]
    config = connector.Config(shop_domain="sample.myshoplaza.com")
    credentials = connector.Credentials(
        sdk.SecretString("" if name.endswith("-defect") else "test-shoplazza-token")
    )
    return connector.Client(config, conformance.static_credentials(CONNECTION, credentials), client)


ADAPTER = conformance.PythonOperationAdapter("shoplazza", CONNECTION, connector, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare provider requests and decisions with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
