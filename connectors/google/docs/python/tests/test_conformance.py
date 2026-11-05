# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay exact Go-recorded Google Docs provider decisions through Python."""

import sys
from pathlib import Path
from typing import Any

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.google_docs as docs

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/google/docs/scenarios.json")
CONNECTION = sdk.ConnectionRef("google", "policy-docs")
ADAPTER = PythonOperationAdapter(
    "google-docs",
    CONNECTION,
    docs,
    lambda http_client, _: docs.Client(
        docs.Config(docs_endpoint="http://127.0.0.1:41300", drive_endpoint="http://127.0.0.1:41300"),
        sdk.StaticCredentialProvider(
            {CONNECTION: docs.Credentials(access_token=sdk.SecretString("docs-token-SENTINEL"))}
        ),
        http_client,
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match recorded requests, branches, retries, and SDK-encoded outputs."""

    async def invoke(values: dict[str, Any], recorder: object, context: object) -> object:
        if values["operation"] != "replaceDocumentText" or not values["input"].get("placeholders"):
            return await ADAPTER.invoke(values, recorder, context)
        adapted = {**values, "input": {**values["input"], "placeholders": tuple(
            docs.PlaceholderReplacement(**item) for item in values["input"]["placeholders"]
        )}}
        return await ADAPTER.invoke(adapted, recorder, context)

    await replay(scenario, invoke)
