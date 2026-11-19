# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-verified ElevenLabs provider scenarios through Python."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.elevenlabs as elevenlabs

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/elevenlabs/scenarios.json")
CONNECTION = sdk.ConnectionRef("elevenlabs", "elevenlabs")
ADAPTER = conformance.PythonOperationAdapter(
    "elevenlabs", CONNECTION, elevenlabs,
    lambda http_client, _: elevenlabs.Client(
        elevenlabs.Config(base_url="http://127.0.0.1"),
        conformance.static_credentials(CONNECTION, elevenlabs.Credentials(sdk.SecretString("test-key-123"))),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare requests and typed outcomes with the recorded Go behavior."""
    await conformance.replay(scenario, ADAPTER.invoke)
