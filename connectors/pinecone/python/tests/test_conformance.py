# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Pinecone provider exchanges through the Python connector."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.pinecone as pinecone

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/pinecone/scenarios.json")
CONNECTION = sdk.ConnectionRef("pinecone", "pinecone")


class _ConformanceUpsertVectorsOperation:
    """Construct the generated nested Vector models from fixture JSON."""

    def __init__(self, client: pinecone.Client) -> None:
        self.operation = client.upsert_vectors()

    def definition(self):
        return self.operation.definition()

    def idempotency_key(self, call_id, input):
        return self.operation.idempotency_key(call_id, input)

    async def invoke(self, call, input):
        vectors = tuple(
            pinecone.Vector(**vector) if isinstance(vector, dict) else vector
            for vector in input.vectors
        )
        return await self.operation.invoke(call, pinecone.UpsertVectorsInput(input.namespace, vectors))


ADAPTER = PythonOperationAdapter(
    "pinecone",
    CONNECTION,
    pinecone,
    lambda http_client, _: pinecone.Client(
        pinecone.Config(base_url="http://127.0.0.1:12345"),
        sdk.StaticCredentialProvider(
            {CONNECTION: pinecone.Credentials(sdk.SecretString("pinecone-test-key"))}
        ),
        http_client,
    ),
    operation_types={"upsertVectors": _ConformanceUpsertVectorsOperation},
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare complete requests and SDK-encoded outcomes with the Go recording."""
    await replay(scenario, ADAPTER.invoke)
