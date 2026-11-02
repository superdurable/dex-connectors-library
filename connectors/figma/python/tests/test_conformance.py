# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Figma responses through the Python connector."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.figma as figma

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/figma/scenarios.json")
CONNECTION = sdk.ConnectionRef("figma", "figma")
ADAPTER = conformance.PythonOperationAdapter(
    "figma", CONNECTION, figma,
    lambda http_client, _: figma.Client(
        figma.Config(base_url="http://127.0.0.1:21105/v1"),
        conformance.static_credentials(CONNECTION, figma.Credentials(sdk.SecretString("test-secret-key"))),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match the Go request and branch for each Figma scenario."""
    await conformance.replay(scenario, ADAPTER.invoke)
