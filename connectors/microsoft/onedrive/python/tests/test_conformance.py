# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Microsoft Graph operation scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.microsoft_onedrive as onedrive

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay, static_credentials  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/microsoft/onedrive/scenarios.json")
CONNECTION = sdk.ConnectionRef("microsoft", "onedrive-files")
ADAPTER = PythonOperationAdapter(
    "microsoft-onedrive",
    CONNECTION,
    onedrive,
    lambda http_client, _: onedrive.Client(
        onedrive.Config(endpoint="http://127.0.0.1:12345"),
        static_credentials(
            CONNECTION, onedrive.Credentials(access_token=sdk.SecretString("graph-token-SENTINEL"))
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Require the Python operations to match Go requests and decisions."""
    await replay(scenario, ADAPTER.invoke, "/v1.0")
