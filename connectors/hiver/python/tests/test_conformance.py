# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay exact Go-recorded Hiver exchanges through the Python SDK."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.hiver as hiver

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/hiver/scenarios.json")
CONNECTION = sdk.ConnectionRef("hiver", "hiver-test")
ADAPTER = PythonOperationAdapter(
    "hiver",
    CONNECTION,
    hiver,
    lambda http_client, _: hiver.Client(
        hiver.Config(request_interval_milliseconds=1),
        sdk.StaticCredentialProvider(
            {CONNECTION: hiver.Credentials(sdk.SecretString("hiverTestKey0123456789"))}
        ),
        http_client,
        api_base_url="http://127.0.0.1:12345/v1",
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match every request, provider decision, and exact SDK-encoded output."""
    await replay(scenario, ADAPTER.invoke, "/v1")
