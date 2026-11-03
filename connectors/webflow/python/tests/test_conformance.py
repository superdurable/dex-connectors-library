# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Webflow provider scenarios through the Python port."""
import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest
import dex_connectors.webflow as webflow

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/webflow/scenarios.json")
CONNECTION = sdk.ConnectionRef("webflow", "webflow")
ADAPTER = conformance.PythonOperationAdapter(
    "webflow", CONNECTION, webflow,
    lambda client, _: webflow.Client(
        webflow.Config(base_url="http://127.0.0.1:8848/v2"),
        conformance.static_credentials(CONNECTION, webflow.Credentials(sdk.SecretString("test-token"))),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare requests and decisions with the Go recording."""
    await conformance.replay(scenario, ADAPTER.invoke)
