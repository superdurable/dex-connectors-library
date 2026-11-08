# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded People API decisions in Python."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.google_contacts as contacts

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay, static_credentials  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/google/contacts/scenarios.json")
CONNECTION = sdk.ConnectionRef("google-contacts", "google-contacts")


def create_client(http_client: httpx.AsyncClient, recorder: object) -> contacts.Client:
    """Bind inert Go fixture credentials to the replay transport."""
    credentials = contacts.Credentials(
        "test-client",
        sdk.SecretString("test-client-secret"),
        sdk.SecretString("test-contacts-token"),
        sdk.SecretString("test-refresh-token"),
    )
    return contacts.Client(
        contacts.Config(base_url="http://127.0.0.1:12345"),
        static_credentials(CONNECTION, credentials),
        http_client,
    )


ADAPTER = PythonOperationAdapter("google-contacts", CONNECTION, contacts, create_client, with_context=True)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go."""
    await replay(scenario, ADAPTER.invoke)
