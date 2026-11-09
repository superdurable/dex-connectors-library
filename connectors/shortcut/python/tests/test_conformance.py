# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Shortcut provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.shortcut as shortcut

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/shortcut/scenarios.json")
CONNECTION = sdk.ConnectionRef("shortcut", "shortcut")
TEST_CREDENTIALS = shortcut.Credentials(sdk.SecretString("sample-shortcut-token"))
ADAPTER = conformance.PythonOperationAdapter(
    "shortcut",
    CONNECTION,
    shortcut,
    lambda http_client, _recorder: shortcut.Client(
        shortcut.Config(base_url="http://127.0.0.1:8848/api/v3"),
        conformance.static_credentials(CONNECTION, TEST_CREDENTIALS),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/api/v3")
