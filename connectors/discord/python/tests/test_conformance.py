# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Discord provider scenarios through the Python SDK."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.discord as discord

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/discord/scenarios.json")
CONNECTION = sdk.ConnectionRef("discord", "discord")
ADAPTER = conformance.PythonOperationAdapter(
    "discord",
    CONNECTION,
    discord,
    lambda client, _: discord.Client(
        discord.Config(endpoint="http://127.0.0.1:22048"),
        conformance.static_credentials(
            CONNECTION, discord.Credentials(sdk.SecretString("MTAxTESTbot.token-part.0123456789abcdef"))
        ),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare provider requests and Dex-encoded outcomes with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
