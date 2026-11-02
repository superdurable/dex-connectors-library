# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.newsapi as newsapi

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/newsapi/scenarios.json")
CONNECTION = sdk.ConnectionRef("newsapi", "newsapi")
ADAPTER = conformance.PythonOperationAdapter(
    "newsapi", CONNECTION, newsapi,
    lambda client, _: newsapi.Client(
        newsapi.Config(base_url="http://127.0.0.1:8848/v2"),
        conformance.static_credentials(
            CONNECTION, newsapi.Credentials(sdk.SecretString("0123456789abcdef0123456789abcdef"))),
        client,
    ),
)
@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check provider requests and operation outcomes against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
