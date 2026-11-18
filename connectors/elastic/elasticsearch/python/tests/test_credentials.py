# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Classify credential failures as terminal or transient before any request."""

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    OperationRef,
    ReauthorizationRequiredError,
)
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
)
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError

import dex_connectors.elastic_elasticsearch as elasticsearch

CONNECTION = ConnectionRef("elasticsearch", "elasticsearch")


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "is_terminal"),
    [
        (ReauthorizationRequiredError(), True),
        (ObjectNotFoundError(), True),
        (ConnectionNotSelectedError(), True),
        (CredentialBrokerRejectedError(), True),
        (CredentialBrokerUnavailableError(), False),
        (ValueError("connection is not configured"), False),
        (RuntimeError("unlisted"), False),
    ],
)
async def test_credential_failures_select_defect_only_when_terminal(
    error: Exception, is_terminal: bool
) -> None:
    requests: list[httpx.Request] = []

    class FailingCredentials:
        async def resolve(self, call: Call) -> elasticsearch.Credentials:
            raise error

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json={})

    client = elasticsearch.Client(
        elasticsearch.Config(cluster_url="http://127.0.0.1:12345"),
        FailingCredentials(),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )
    call = Call(CallID("call-1"), CONNECTION, OperationRef("elasticsearch", "indexDocument"))
    attempts = [
        await client.index_document().invoke(
            call, elasticsearch.IndexDocumentInput("orders", "order-1", {"total": 1})
        ),
        await client.delete_document().invoke(call, elasticsearch.DeleteDocumentInput("orders", "order-1")),
        await client.search_documents().invoke(
            call, elasticsearch.SearchDocumentsInput("orders", "total:1")
        ),
    ]
    for attempt in attempts:
        if is_terminal:
            assert attempt.branch == "defect"
        else:
            assert attempt.kind == "retry"
            assert attempt.failure is not None and attempt.failure.kind == FailureKind.AVAILABILITY
    assert requests == []
