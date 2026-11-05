# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Bitbucket provider scenarios through the Python port."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.atlassian_bitbucket as bitbucket

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/atlassian/bitbucket/scenarios.json")
CONNECTION = sdk.ConnectionRef("bitbucket", "bitbucket")
ADAPTER = conformance.PythonOperationAdapter(
    "bitbucket", CONNECTION, bitbucket,
    lambda client, _: bitbucket.Client(
        bitbucket.Config(base_url="http://127.0.0.1:8848/2.0"),
        conformance.static_credentials(CONNECTION, bitbucket.Credentials(sdk.SecretString("test-token"))),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python provider requests and outcomes with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
