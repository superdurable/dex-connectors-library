# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Cainiao provider requests and decisions."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.cainiao as cainiao

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/cainiao/scenarios.json")
CONNECTION = sdk.ConnectionRef("cainiao", "cainiao")
_DATE = datetime(2026, 10, 22, 7, 26, 40, tzinfo=UTC)

ADAPTER = conformance.PythonOperationAdapter(
    "cainiao",
    CONNECTION,
    cainiao,
    lambda http_client, recorder: cainiao.Client(
        cainiao.Config(
            base_url="http://127.0.0.1:12345/router/rest",
            max_response_bytes=8 if recorder.scenario["name"] == "queryWaybillTrace-oversized" else 65536,
        ),
        conformance.static_credentials(
            CONNECTION,
            cainiao.Credentials("123456", sdk.SecretString("secretKey"), sdk.SecretString("sessionToken")),
        )
        if recorder.scenario["name"] != "queryWaybillTrace-missing-credentials"
        else sdk.StaticCredentialProvider({}),
        http_client,
        clock=lambda: _DATE,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact Go requests, outputs, branches, retries, and request counts."""
    await conformance.replay(scenario, ADAPTER.invoke)
