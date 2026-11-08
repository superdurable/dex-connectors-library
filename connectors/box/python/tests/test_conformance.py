# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-verified Box requests and provider decisions."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.box as box

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay, static_credentials  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/box/scenarios.json")
CONNECTION = sdk.ConnectionRef("box", "box")


def create_client(http_client: object, recorder: object) -> box.Client:
    """Bind the exact Go fixture credential to an isolated provider transport."""
    import httpx

    assert isinstance(http_client, httpx.AsyncClient)
    assert hasattr(recorder, "scenario")
    scenario = recorder.scenario
    assert isinstance(scenario, dict)
    credentials = box.Credentials(
        "client-id",
        sdk.SecretString("client-secret"),
        sdk.SecretString("test-token"),
        sdk.SecretString("refresh-token"),
    )
    return box.Client(
        box.Config(
            base_url="http://127.0.0.1",
            upload_base_url="http://127.0.0.1",
            max_response_bytes=8 if scenario["name"] == "downloadFile-invalidResponse" else 2_097_152,
            max_download_bytes=8,
            max_upload_bytes=8,
        ),
        static_credentials(CONNECTION, credentials),
        http_client,
    )


ADAPTER = PythonOperationAdapter("box", CONNECTION, box, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Require Python branches, retries, outputs, and requests to match Go."""
    await replay(scenario, ADAPTER.invoke)
