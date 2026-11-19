# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded LianLianPay account balance scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.lianlianpay as lianlianpay
from tests.synthetic_key import synthetic_private_key

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/lianlianpay/scenarios.json")
CONNECTION = sdk.ConnectionRef("lianlianpay", "lianlianpay")


ADAPTER = conformance.PythonOperationAdapter(
    "lianlianpay",
    CONNECTION,
    lianlianpay,
    lambda http_client, recorder: lianlianpay.Client(
        lianlianpay.Config(
            max_response_bytes=16 if recorder.scenario["name"].endswith("oversized") else 262144
        ),
        conformance.static_credentials(
            CONNECTION,
            lianlianpay.Credentials("2020042200284052", sdk.SecretString(synthetic_private_key())),
        ),
        http_client,
        lambda: conformance.HeartbeatContext(fixed_clock=recorder.scenario["fixedClock"]).fixed_clock,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare requests, outputs, and decisions with the Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
