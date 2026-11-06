# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Spotify provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.spotify as spotify

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/spotify/scenarios.json")
CONNECTION = sdk.ConnectionRef("spotify", "playlist-reader")
ADAPTER = PythonOperationAdapter(
    "spotify",
    CONNECTION,
    spotify,
    lambda client, _: spotify.Client(
        spotify.Config(endpoint="http://127.0.0.1/v1"),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: spotify.Credentials(
                    "", sdk.SecretString(""), sdk.SecretString("one-use-token"), sdk.SecretString("")
                )
            }
        ),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check the provider request and SDK-coded attempt against Go."""
    await replay(scenario, ADAPTER.invoke)
