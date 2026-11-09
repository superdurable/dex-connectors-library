# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Elasticsearch requests and Dex decisions."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.elastic_elasticsearch as elasticsearch

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/elastic/elasticsearch/scenarios.json")
CONNECTION = sdk.ConnectionRef("elasticsearch", "elasticsearch")
ADAPTER = PythonOperationAdapter(
    "elasticsearch", CONNECTION, elasticsearch,
    lambda http_client, _: elasticsearch.Client(
        elasticsearch.Config(cluster_url="http://127.0.0.1:12345"),
        sdk.StaticCredentialProvider({
            CONNECTION: elasticsearch.Credentials("dex_reader", sdk.SecretString("fake-password"))
        }),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python provider requests and Dex decisions with Go recordings."""
    await replay(scenario, ADAPTER.invoke)
