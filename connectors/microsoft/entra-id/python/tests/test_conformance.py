# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Microsoft Graph requests through the Python package."""

import sys
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.microsoft_entra_id as entra

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/microsoft/entra-id/scenarios.json")
CONNECTION = sdk.ConnectionRef("microsoft", "entra-conformance")


class RenewalCredentials:
    """Return a fresh inert token only after the Graph 401 rejection."""

    async def resolve(self, call: sdk.Call) -> entra.Credentials:
        return entra.Credentials(
            auth_method_id="microsoft-oauth", access_token=sdk.SecretString("stale-token")
        )

    async def resolve_after_rejection(self, call: sdk.Call) -> entra.Credentials:
        return entra.Credentials(
            auth_method_id="microsoft-oauth", access_token=sdk.SecretString("fresh-token")
        )


def client_for(http_client: httpx.AsyncClient, recorder: Any) -> entra.Client:
    """Bind inert credentials and deterministic random bytes to the replay transport."""
    scenario = recorder.scenario
    config = entra.Config(
        max_response_bytes=2 if scenario["name"] == "removeUserFromGroup-invalid-response" else 4194304
    )
    source = conformance.SyntheticRandomSource(scenario["randomSeed"]) if scenario.get("randomSeed") else None
    credentials = (
        RenewalCredentials()
        if scenario["name"] == "getUser-401-renewed"
        else conformance.static_credentials(
            CONNECTION, entra.Credentials(access_token=sdk.SecretString("entra-token"))
        )
    )
    return entra.Client(
        config,
        credentials,
        http_client,
        local_provider_url="http://127.0.0.1:12345",
        random_source=source,
    )


ADAPTER = conformance.PythonOperationAdapter(
    "microsoft-entra-id", CONNECTION, entra, client_for, with_context=True
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match each Go request, branch, Retry, and encoded output."""

    async def invoke(scenario: dict[str, Any], recorder: Any, context: Any) -> Any:
        if "propagation" in scenario["name"]:
            context.first_attempt_at = datetime.now(UTC)
        return await ADAPTER.invoke(scenario, recorder, context)

    await conformance.replay(scenario, invoke, "/v1.0")
