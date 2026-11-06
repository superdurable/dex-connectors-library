# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Help Scout provider scenarios through the Python SDK."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.helpscout as helpscout

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/helpscout/scenarios.json")
CONNECTION = sdk.ConnectionRef("helpscout", "helpscout-conformance")
ADAPTER = PythonOperationAdapter(
    "helpscout",
    CONNECTION,
    helpscout,
    lambda http_client, _: helpscout.Client(
        helpscout.Config(),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: helpscout.Credentials(
                    "inert-app-id",
                    sdk.SecretString("inert-app-secret"),
                    sdk.SecretString("SENTINEL-HELPSCOUT-ACCESS-TOKEN"),
                    sdk.SecretString("inert-webhook-secret"),
                )
            }
        ),
        http_client,
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check exact requests, branch decisions, retry and encoded output."""
    await replay(scenario, ADAPTER.invoke)
