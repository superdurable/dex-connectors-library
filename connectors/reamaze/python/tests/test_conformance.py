# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Re:amaze provider outcomes through the Python SDK path."""

import sys
from dataclasses import dataclass
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.reamaze as reamaze

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/reamaze/scenarios.json")
CONNECTION = sdk.ConnectionRef("reamaze", "reamaze-test")


@dataclass(frozen=True, slots=True)
class _FixtureCreateConversationInput(reamaze.CreateConversationInput):
    """Decode the fixture requester object into the generated input type."""

    def __post_init__(self) -> None:
        if isinstance(self.requester, dict):
            object.__setattr__(self, "requester", reamaze.ConversationRequesterInput(**self.requester))


class _FixtureModels:
    """Select typed fixture inputs without changing published models."""

    def __getattr__(self, name: str) -> object:
        if name == "CreateConversationInput":
            return _FixtureCreateConversationInput
        return getattr(reamaze, name)


ADAPTER = PythonOperationAdapter(
    "reamaze",
    CONNECTION,
    _FixtureModels(),
    lambda client, _: reamaze.Client(
        reamaze.Config("acme"),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: reamaze.Credentials(
                    "agent@acme.example.com", sdk.SecretString("reamazeTestToken0123456789")
                )
            }
        ),
        client,
        "http://127.0.0.1:12345/api/v1",
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare each exchange, branch, retry, and exact SDK-encoded output to Go."""
    await replay(scenario, ADAPTER.invoke, "/api/v1")
