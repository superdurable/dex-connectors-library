# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Microsoft To Do requests and outcomes."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.microsoft_todo as todo

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/microsoft/todo/scenarios.json")
CONNECTION = sdk.ConnectionRef("microsoft", "microsoft-todo")


def create_client(http_client: object, recorder: conformance.ExchangeRecorder) -> todo.Client:
    """Use scenario-local synthetic credentials with the replay transport."""
    import httpx

    assert isinstance(http_client, httpx.AsyncClient)
    token = "" if recorder.scenario["name"] == "listTaskLists-defect" else "sample-access-token"
    credentials = todo.Credentials(
        "sample-client-id", sdk.SecretString("sample-client-secret"),
        sdk.SecretString(token), sdk.SecretString("sample-refresh-token"),
    )
    return todo.Client(todo.Config(), conformance.static_credentials(CONNECTION, credentials), http_client)


ADAPTER = conformance.PythonOperationAdapter("microsoft-todo", CONNECTION, todo, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match Go's requests, outputs, and retry or branch decisions."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/v1.0")
