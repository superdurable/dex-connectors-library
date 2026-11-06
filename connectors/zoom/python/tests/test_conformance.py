# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Zoom provider exchanges through Python SDK calls."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.zoom as zoom

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/zoom/scenarios.json")
CONNECTION = sdk.ConnectionRef("zoom", "zoom-test")
ADAPTER = PythonOperationAdapter(
    "zoom",
    CONNECTION,
    zoom,
    lambda http_client, _: zoom.Client(
        zoom.Config(endpoint="https://zoom.example/v2"),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: zoom.Credentials(
                    "zoom-client-id",
                    sdk.SecretString("zoom-client-secret"),
                    sdk.SecretString("zoom-test-access-token"),
                    sdk.SecretString("zoom-refresh-token"),
                )
            }
        ),
        http_client,
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match each exact request, provider decision, and SDK-encoded output."""
    await replay(scenario, ADAPTER.invoke, "/v2")
