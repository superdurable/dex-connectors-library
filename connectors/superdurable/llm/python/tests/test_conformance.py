# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded provider requests and Dex SDK outputs."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.superdurable_llm as llm

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/superdurable/llm/scenarios.json")
CONNECTION = sdk.ConnectionRef("llm", "conformance")


def _client(http_client, recorder):
    provider = recorder.scenario["name"].split("/", 1)[0]
    base_url = "http://127.0.0.1"
    return llm.Client(
        llm.Config(
            provider, base_url + "/v1" if provider == "openai-compatible" else "", "mock-model", "", 0
        ),
        conformance.static_credentials(CONNECTION, llm.Credentials(sdk.SecretString("sk-conformance-inert"))),
        http_client,
        base_url_for_test=base_url,
    )


ADAPTER = conformance.PythonOperationAdapter("llm", CONNECTION, llm, _client, with_context=True)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Require exact Go request and operation decisions."""
    await conformance.replay(scenario, ADAPTER.invoke)
