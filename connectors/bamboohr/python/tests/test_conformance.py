# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded BambooHR provider behavior with the Python package."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.bamboohr as bamboohr

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/bamboohr/scenarios.json")
CONNECTION = sdk.ConnectionRef("bamboohr", "bamboohr-test")
ADAPTER = PythonOperationAdapter(
    "bamboohr",
    CONNECTION,
    bamboohr,
    lambda client, _: bamboohr.Client(
        bamboohr.Config("acme"),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: bamboohr.Credentials(
                    sdk.SecretString("0123456789abcdef" + "0123456789abcdef01234567")
                )
            }
        ),
        client,
        "http://127.0.0.1:12345/api/v1",
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match every provider request, SDK output, and retry decision with Go."""
    await replay(scenario, ADAPTER.invoke, "/api/v1")
