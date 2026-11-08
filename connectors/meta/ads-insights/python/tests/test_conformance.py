# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Meta Ads Insights provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.meta_ads_insights as metaadsinsights

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = [
    scenario
    for scenario in conformance.load_scenarios(
        ROOT / "conformance/connectors/meta/ads-insights/scenarios.json"
    )
    if scenario["operation"] in {"getAd"}
]
CONNECTION = sdk.ConnectionRef("meta-ads-insights", "meta-ads-insights")
ADAPTER = conformance.PythonOperationAdapter(
    "meta-ads-insights",
    CONNECTION,
    metaadsinsights,
    lambda http_client, recorder: metaadsinsights.Client(
        metaadsinsights.Config(base_url="http://127.0.0.1:8848/v25.0"),
        conformance.static_credentials(
            CONNECTION, metaadsinsights.Credentials(sdk.SecretString("test-marketing-token"))
        ),
        http_client,
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/v25.0")
