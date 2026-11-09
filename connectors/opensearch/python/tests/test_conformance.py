# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded OpenSearch requests and Dex decisions."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.opensearch as opensearch

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/opensearch/scenarios.json")
CONNECTION = sdk.ConnectionRef("opensearch", "opensearch")
ADAPTER = PythonOperationAdapter(
    "opensearch", CONNECTION, opensearch,
    lambda http_client, _: opensearch.Client(
        opensearch.Config(cluster_url="http://127.0.0.1:12345"),
        sdk.StaticCredentialProvider({
            CONNECTION: opensearch.Credentials("dex_reader", sdk.SecretString("fake-password"))
        }),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python provider requests and Dex decisions with Go recordings."""
    await replay(scenario, ADAPTER.invoke)
