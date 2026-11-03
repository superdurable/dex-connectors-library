# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Contentful provider outcomes through the Python port."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.contentful as contentful

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/contentful/scenarios.json")
CONNECTION = sdk.ConnectionRef("contentful", "contentful")
ADAPTER = conformance.PythonOperationAdapter(
    "contentful", CONNECTION, contentful,
    lambda client, _: contentful.Client(
        contentful.Config(base_url="http://127.0.0.1:8848"),
        conformance.static_credentials(
            CONNECTION, contentful.Credentials(sdk.SecretString("test-token"))),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests and decisions with the recorded Go connector."""
    await conformance.replay(scenario, ADAPTER.invoke)
