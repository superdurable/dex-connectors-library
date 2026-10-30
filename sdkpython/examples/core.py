# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Phase 1a API families without Dex Worker registration."""

import asyncio
import json
from datetime import UTC, datetime
from uuid import uuid4

import httpx

from dex_connectors.sdk import (
    CallID,
    ConnectionRef,
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    Failure,
    FailureKind,
    MutationAttempt,
    OperationRef,
    QueryResult,
    Receipt,
    SecretString,
    default_connector_step_execute_retry_policy,
)
from dex_connectors.sdk.connectormock import Case, ScriptedOperation
from dex_connectors.sdk.projectconfig import (
    Configuration,
    ConfigurationStore,
    ConnectionConfiguration,
    InMemoryObjectStore,
    Scope,
    load_project_from_object_store,
)
from dex_connectors.sdk.providerhttp import new_provider_http_client, read_bounded_body


async def main() -> None:
    """Exercise identities, failures, UI, HTTP, project storage, and mocks."""
    call_id = CallID(str(uuid4()))
    call_id.validate()
    connection = ConnectionRef("newsapi", "default")
    operation = OperationRef("newsapi", "listHeadlines")
    receipt = Receipt(call_id, "newsapi", datetime.now(UTC))
    result: QueryResult[dict[str, list[str]]] = QueryResult("headlines", {"titles": []}, receipt)
    assert json.loads(json.dumps(result.to_wire()))["receipt"]["callId"] == call_id
    failure = Failure(FailureKind.RATE_LIMIT, "newsapi", "listHeadlines", "provider throttled")
    assert MutationAttempt.retry(failure).kind == "retry"
    assert default_connector_step_execute_retry_policy().maximum_attempts == 0
    assert str(SecretString("example-secret")) == "[REDACTED]"

    configuration_ui = ConnectorConfigurationUI(
        (
            ConnectorUIUnit(
                "headlineLimit",
                "integerField",
                "Limit",
                "Choose the number of headlines.",
                True,
                (ConnectorUIBinding("value", "/limit"),),
            ),
        )
    )
    configuration_ui.validate()

    async def provider_response(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={"titles": []})

    async with new_provider_http_client(transport=httpx.MockTransport(provider_response)) as http_client:
        response = await http_client.get("https://provider.example/headlines")
        assert json.loads(await read_bounded_body(response, 4096)) == {"titles": []}

    scope = Scope("example", "preview")
    objects = InMemoryObjectStore()
    configurations = ConfigurationStore(objects, scope)
    ordinary = Configuration(
        scope, 1, (ConnectionConfiguration("newsapi", "default", "example", "newsapi", {}),)
    )
    written, _ = await configurations.write_configuration(0, ordinary)
    snapshot = await configurations.freeze_configuration(written.revision)
    project = await load_project_from_object_store(objects, scope, snapshot)
    assert project.configuration.connections[0].connection_name == "default"

    mock = ScriptedOperation[dict[str, str], dict[str, list[str]]](operation, ("headlines", "defect"))
    mock.respond(Case.branch_result("headlines", {"titles": []}))
    assert mock.answer("flow-1", call_id, connection, {"country": "US"}).branch == "headlines"


if __name__ == "__main__":
    asyncio.run(main())
