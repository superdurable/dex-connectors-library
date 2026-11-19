# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Supabase provider scenarios through the existing Python port."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.supabase as supabase

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/supabase/scenarios.json")
CONNECTION = sdk.ConnectionRef("supabase", "supabase")
ADAPTER = conformance.PythonOperationAdapter(
    "supabase", CONNECTION, supabase,
    lambda client, _: supabase.Client(
        supabase.Config(base_url="http://127.0.0.1:28808/rest/v1"),
        conformance.static_credentials(
            CONNECTION, supabase.Credentials(sdk.SecretString("test-token"))),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match Go provider requests, branches, retry decisions, and outputs."""
    await conformance.replay(scenario, ADAPTER.invoke)
