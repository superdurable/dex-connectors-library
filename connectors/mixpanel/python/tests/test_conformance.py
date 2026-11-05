# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Mixpanel provider requests through the Python connector."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.mixpanel as mixpanel

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/mixpanel/scenarios.json")
CONNECTION = sdk.ConnectionRef("mixpanel", "mixpanel")
ADAPTER = PythonOperationAdapter(
    "mixpanel",
    CONNECTION,
    mixpanel,
    lambda http_client, _: mixpanel.Client(
        mixpanel.Config(
            project_id=12345,
            base_url="http://127.0.0.1:12345",
            export_base_url="http://127.0.0.1:12345/api/2.0",
        ),
        sdk.StaticCredentialProvider({
            CONNECTION: mixpanel.Credentials(
                sdk.SecretString("service-user"), sdk.SecretString("service-secret")
            )
        }),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare complete requests and outcomes with the Go recording."""
    await replay(scenario, ADAPTER.invoke)
