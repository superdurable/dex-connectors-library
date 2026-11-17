# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded CTYun signed requests through Python."""

import sys
from datetime import datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.ctyun as ctyun

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/ctyun/scenarios.json")
CONNECTION = sdk.ConnectionRef("ctyun", "ctyun")
REQUEST_ID = "123e4567-e89b-42d3-a456-426614174000"


def new_client(http_client: object, recorder: object) -> ctyun.Client:
    """Supply inert keys and exact Go fixture time and request ID."""
    scenario = recorder.scenario
    name = scenario["name"]
    is_missing = name == "listKnowledgeBases-missing-credentials"
    limit = 4 if name == "listKnowledgeBases-oversize" else 2_097_152
    fixed_clock = datetime.fromisoformat(scenario["fixedClock"].replace("Z", "+00:00"))

    def request_id() -> str:
        if name == "listKnowledgeBases-id-generation-failure":
            raise OSError("synthetic request ID failure")
        return REQUEST_ID

    return ctyun.Client(
        ctyun.Config(max_response_bytes=limit),
        conformance.static_credentials(
            CONNECTION,
            ctyun.Credentials(
                sdk.SecretString("" if is_missing else "AKIDEXAMPLE"),
                sdk.SecretString("" if is_missing else "SKEXAMPLE"),
                sdk.SecretString("" if is_missing else "tenant-example"),
            ),
        ),
        http_client,
        clock=lambda: fixed_clock,
        request_id_generator=request_id,
    )


ADAPTER = conformance.PythonOperationAdapter("ctyun", CONNECTION, ctyun, new_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check signed headers, output, branches, Retry, and request counts against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
