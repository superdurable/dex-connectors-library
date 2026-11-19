# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Fanyigou account balance scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.fanyigou as fanyigou

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/fanyigou/scenarios.json")
CONNECTION = sdk.ConnectionRef("fanyigou", "fanyigou")
ADAPTER = conformance.PythonOperationAdapter(
    "fanyigou", CONNECTION, fanyigou,
    lambda http_client, recorder: fanyigou.Client(
        fanyigou.Config(
            max_response_bytes=16 if recorder.scenario["name"].endswith("oversized") else 1048576
        ),
        conformance.static_credentials(
            CONNECTION,
            fanyigou.Credentials(
                sdk.SecretString("" if recorder.scenario["name"].endswith("defect") else "test-app"),
                sdk.SecretString("" if recorder.scenario["name"].endswith("defect") else "test-private-key"),
            ),
        ),
        http_client,
        conformance.SyntheticRandomSource(
            recorder.scenario.get("randomSeed", "SYNTHETIC_CONFORMANCE_fanyigou_defect")
        ).read,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare requests, outputs, and decisions with the Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
