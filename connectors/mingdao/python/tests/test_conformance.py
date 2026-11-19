# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay exact Go-recorded Mingdao requests and outcomes."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.mingdao as provider

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/mingdao/scenarios.json")
CONNECTION = sdk.ConnectionRef("mingdao", "mingdao")


def create_client(http_client: object, recorder: conformance.ExchangeRecorder) -> provider.Client:
    """Bind inert fixture credentials, including the missing-credential case."""
    import httpx

    assert isinstance(http_client, httpx.AsyncClient)
    is_defect = recorder.scenario["name"].endswith("-defect")
    credentials = provider.Credentials(
        *(
            (
                sdk.SecretString(""),
                sdk.SecretString(""),
            )
            if is_defect
            else (
                sdk.SecretString("app-secret"),
                sdk.SecretString("sign-secret"),
            )
        )
    )
    return provider.Client(
        provider.Config(), conformance.static_credentials(CONNECTION, credentials), http_client
    )


ADAPTER = conformance.PythonOperationAdapter("mingdao", CONNECTION, provider, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match every Go request, output, branch, and retry decision exactly."""
    await conformance.replay(scenario, ADAPTER.invoke)
