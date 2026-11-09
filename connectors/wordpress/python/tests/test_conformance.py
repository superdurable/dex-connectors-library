# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded WordPress requests, branches, retries, and checkpoints."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.wordpress as wordpress

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/wordpress/scenarios.json")
CONNECTION = sdk.ConnectionRef("wordpress", "wordpress")
ADAPTER = PythonOperationAdapter(
    "wordpress", CONNECTION, wordpress,
    lambda http_client, _: wordpress.Client(
        wordpress.Config(site_url="http://127.0.0.1:12345"),
        sdk.StaticCredentialProvider({
            CONNECTION: wordpress.Credentials("editor", sdk.SecretString("synthetic test password"))
        }),
        http_client,
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python provider requests and Dex decisions with exact Go recordings."""
    await replay(scenario, ADAPTER.invoke)
