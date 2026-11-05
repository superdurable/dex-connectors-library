# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay DocuSign's Go-recorded provider decisions through the Python port."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.docusign as docusign

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/docusign/scenarios.json")
CONNECTION = sdk.ConnectionRef("docusign", "esignature")
ACCOUNT_NOW = datetime(2026, 10, 4, 12, tzinfo=UTC)
ADAPTER = PythonOperationAdapter(
    "docusign", CONNECTION, docusign,
    lambda client, _: docusign.Client(
        docusign.Config(max_document_bytes=64),
        sdk.StaticCredentialProvider({CONNECTION: docusign.Credentials(
            oauth_client_id="integration-key",
            oauth_client_secret=sdk.SecretString("SENTINEL-DOCUSIGN-CLIENT-SECRET"),
            access_token=sdk.SecretString("SENTINEL-DOCUSIGN-ACCESS-TOKEN"),
            refresh_token=sdk.SecretString("SENTINEL-DOCUSIGN-REFRESH-TOKEN"),
        )}), client, lambda: ACCOUNT_NOW,
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare every provider exchange, branch, retry, and exact encoded output."""
    await replay(scenario, ADAPTER.invoke)
