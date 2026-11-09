# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Ads provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.linkedin_ads as linkedin_ads

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/linkedin/ads/scenarios.json")
CONNECTION = sdk.ConnectionRef("linkedin-ads", "linkedin-ads")
ADAPTER = conformance.PythonOperationAdapter(
    "linkedin-ads",
    CONNECTION,
    linkedin_ads,
    lambda http_client, recorder: linkedin_ads.Client(
        linkedin_ads.Config(
            base_url="http://127.0.0.1:8848",
            max_response_bytes=16
            if str(recorder.scenario["name"]).startswith("listAdAccounts-oversized")
            else 2_097_152,
        ),
        conformance.static_credentials(
            CONNECTION,
            linkedin_ads.Credentials(
                "synthetic-client-id",
                sdk.SecretString("synthetic-client-secret"),
                sdk.SecretString("test-linkedin-ads-token"),
                sdk.SecretString("synthetic-refresh-token"),
            ),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="")
