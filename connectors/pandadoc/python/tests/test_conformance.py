# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded PandaDoc provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.pandadoc as pandadoc

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/pandadoc/scenarios.json")
CONNECTION = sdk.ConnectionRef("pandadoc", "pandadoc")
TEST_CREDENTIALS = pandadoc.Credentials(sdk.SecretString("sample-pandadoc-api-key"))
ADAPTER = conformance.PythonOperationAdapter(
    "pandadoc",
    CONNECTION,
    pandadoc,
    lambda http_client, _recorder: pandadoc.Client(
        pandadoc.Config(base_url="http://127.0.0.1:8848/public/v1"),
        conformance.static_credentials(CONNECTION, TEST_CREDENTIALS),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/public/v1")
