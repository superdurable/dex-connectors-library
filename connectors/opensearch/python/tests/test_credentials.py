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
    SecretString,
)
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
)
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError

import dex_connectors.opensearch as opensearch
from dex_connectors.opensearch._generated.connection import CredentialMaterialError

CONNECTION = ConnectionRef("opensearch", "opensearch")


def _sdk_wrapped_material_error() -> ValueError:
    """Build the ValueError that the SDK project provider raises over a decoder failure."""
    try:
        try:
            raise CredentialMaterialError("OpenSearch credential username is required")
        except CredentialMaterialError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error
    raise AssertionError("unreachable")


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "is_terminal"),
    [
        (ReauthorizationRequiredError(), True),
        (ObjectNotFoundError(), True),
        (ConnectionNotSelectedError(), True),
        (CredentialBrokerRejectedError(), True),
        (CredentialMaterialError("OpenSearch credential username is required"), True),
        (_sdk_wrapped_material_error(), True),
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
        async def resolve(self, call: Call) -> opensearch.Credentials:
            raise error

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json={})

    client = opensearch.Client(
        opensearch.Config(cluster_url="http://127.0.0.1:12345"),
        FailingCredentials(),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )
    call = Call(CallID("call-1"), CONNECTION, OperationRef("opensearch", "indexDocument"))
    attempts = [
        await client.index_document().invoke(
            call, opensearch.IndexDocumentInput("orders", "order-1", {"total": 1})
        ),
        await client.delete_document().invoke(call, opensearch.DeleteDocumentInput("orders", "order-1")),
        await client.search_documents().invoke(call, opensearch.SearchDocumentsInput("orders", "total:1")),
    ]
    for attempt in attempts:
        if is_terminal:
            assert attempt.branch == "defect"
        else:
            assert attempt.kind == "retry"
            assert attempt.failure is not None and attempt.failure.kind == FailureKind.AVAILABILITY
    assert requests == []


@pytest.mark.asyncio
async def test_malformed_stored_credentials_from_the_project_source_are_terminal() -> None:
    from dex_connectors.opensearch._generated.connection import _ProjectCredentialSource

    class MalformedProject:
        async def resolve_credential(self, key: object, call_id: str) -> object:
            return {"username": 42, "password": "secret"}

    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200)

    client = opensearch.Client(
        opensearch.Config(cluster_url="http://127.0.0.1:12345"),
        _ProjectCredentialSource(MalformedProject(), object()),  # type: ignore[arg-type]
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )
    call = Call(CallID("call-1"), CONNECTION, OperationRef("opensearch", "indexDocument"))
    attempt = await client.index_document().invoke(
        call, opensearch.IndexDocumentInput("orders", "order-1", {"total": 1})
    )
    assert attempt.branch == "defect"
    assert requests == []


@pytest.mark.asyncio
async def test_authorization_keeps_its_released_string_result_for_transient_failures() -> None:
    class UnavailableCredentials:
        async def resolve(self, call: Call) -> opensearch.Credentials:
            raise CredentialBrokerUnavailableError()

    client = opensearch.Client(
        opensearch.Config(cluster_url="http://127.0.0.1:12345"), UnavailableCredentials()
    )
    call = Call(CallID("call-1"), CONNECTION, OperationRef("opensearch", "indexDocument"))
    assert await client.authorization(call) == ""
    await client.aclose()


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [405, 409, 425, 451])
async def test_unlisted_write_statuses_retry_as_unknown(status: int) -> None:
    class Credentials:
        async def resolve(self, call: Call) -> opensearch.Credentials:
            return opensearch.Credentials("dex_reader", SecretString("fake-password"))

    client = opensearch.Client(
        opensearch.Config(cluster_url="http://127.0.0.1:12345"),
        Credentials(),
        httpx.AsyncClient(transport=httpx.MockTransport(lambda request: httpx.Response(status, json={}))),
    )
    call = Call(CallID("call-1"), CONNECTION, OperationRef("opensearch", "indexDocument"))
    indexed = await client.index_document().invoke(
        call, opensearch.IndexDocumentInput("orders", "order-1", {"total": 1})
    )
    deleted = await client.delete_document().invoke(call, opensearch.DeleteDocumentInput("orders", "order-1"))
    assert indexed.kind == "retry" and deleted.kind == "retry"
