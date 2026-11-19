# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded FXiaoke account balance scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.fxiaoke as fxiaoke

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/fxiaoke/scenarios.json")
CONNECTION = sdk.ConnectionRef("fxiaoke", "fxiaoke")
ADAPTER = conformance.PythonOperationAdapter(
    "fxiaoke",
    CONNECTION,
    fxiaoke,
    lambda http_client, recorder: fxiaoke.Client(
        fxiaoke.Config(
            employee_id="employee-123",
            max_response_bytes=16 if recorder.scenario["name"].endswith("oversized") else 1048576,
        ),
        conformance.static_credentials(
            CONNECTION,
            fxiaoke.Credentials(
                "" if recorder.scenario["name"].endswith("defect") else "FSAID_123",
                sdk.SecretString("" if recorder.scenario["name"].endswith("defect") else "app-secret"),
                sdk.SecretString("" if recorder.scenario["name"].endswith("defect") else "permanent-code"),
            ),
        ),
        http_client,
        conformance.SyntheticRandomSource(
            recorder.scenario.get("randomSeed", "SYNTHETIC_CONFORMANCE_fxiaoke_defect")
        ).read,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare requests, outputs, and decisions with the Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
