# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Cloudflare provider exchanges recorded by the Go connector."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.cloudflare as cloudflare

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/cloudflare/scenarios.json")
CONNECTION = sdk.ConnectionRef("cloudflare", "cloudflare")
ADAPTER = PythonOperationAdapter(
    "cloudflare", CONNECTION, cloudflare,
    lambda client, recorder: cloudflare.Client(
        cloudflare.Config(base_url="http://127.0.0.1:12345"),
        sdk.StaticCredentialProvider({CONNECTION: cloudflare.Credentials(
            sdk.SecretString("" if recorder.scenario["name"] == "listZones-defect" else "test-apitoken"))}),
        client,
    ),
    operation_types={"purgeCacheByURLs": cloudflare.PurgeCacheByURLsOperation},
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match complete requests, retry decisions, and exact Go outputs."""
    await replay(scenario, ADAPTER.invoke)
