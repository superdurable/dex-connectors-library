# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded YouTube provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.google_youtube as youtube

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/google/youtube/scenarios.json")
CONNECTION = sdk.ConnectionRef("google", "youtube")
ADAPTER = PythonOperationAdapter(
    "google-youtube",
    CONNECTION,
    youtube,
    lambda http_client, _: youtube.Client(
        youtube.Config(endpoint="http://127.0.0.1/youtube/v3"),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: youtube.Credentials(
                    "google-oauth-manage",
                    "inert-client",
                    sdk.SecretString("inert-secret"),
                    sdk.SecretString("youtube-token"),
                    sdk.SecretString("inert-refresh"),
                )
            }
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare complete requests and SDK-coded outcomes with Go."""
    await replay(scenario, ADAPTER.invoke, "")
