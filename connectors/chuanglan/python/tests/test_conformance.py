# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Chuanglan balance requests through the Python provider."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.chuanglan as chuanglan

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/chuanglan/scenarios.json")
CONNECTION = sdk.ConnectionRef("chuanglan", "chuanglan")


def new_client(http_client: object, recorder: object) -> chuanglan.Client:
    """Supply the recorder's inert credentials and response size setting."""
    scenario = recorder.scenario
    name = scenario["name"]
    secret = "" if name == "getBalance-missing-credentials" else "secret-password"
    limit = 4 if name == "getBalance-oversize" else 65_536
    return chuanglan.Client(
        chuanglan.Config(base_url="http://127.0.0.1:8848", max_response_bytes=limit),
        conformance.static_credentials(
            CONNECTION,
            chuanglan.Credentials(sdk.SecretString("N9000123" if secret else ""), sdk.SecretString(secret)),
        ),
        http_client,
    )


ADAPTER = conformance.PythonOperationAdapter("chuanglan", CONNECTION, chuanglan, new_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check exact requests, branches, Retry, and output against the Go recording."""
    await conformance.replay(scenario, ADAPTER.invoke)
