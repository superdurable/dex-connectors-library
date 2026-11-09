# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Native driver branch, retry, and Extended JSON agreement tests."""

from typing import Any

import pytest
from bson import ObjectId
from bson.errors import InvalidBSON
from dex_connectors.sdk import Call, CallID, ConnectionRef, IdempotencyKey, OperationRef, SecretString
from dns.exception import DNSException, Timeout
from dns.resolver import NXDOMAIN
from pymongo.asynchronous import srv_resolver
from pymongo.errors import ConnectionFailure, OperationFailure

from dex_connectors.mongodb import Client, Config, Credentials, FindDocumentsInput


class CredentialSource:
    """Return a synthetic URI without storing it in Flow state."""

    def __init__(self, uri: str = "mongodb://reader:secret@localhost:27017") -> None:
        self.uri = uri

    async def resolve(self, call: Call) -> Credentials:
        return Credentials(SecretString(self.uri))


class FakeCursor:
    """Return scripted native driver documents or a scripted failure."""

    def __init__(self, documents: list[dict[str, Any]] | Exception) -> None:
        self.documents = documents
        self.index = 0

    def __aiter__(self) -> "FakeCursor":
        return self

    async def __anext__(self) -> dict[str, Any]:
        if isinstance(self.documents, Exception):
            raise self.documents
        if self.index >= len(self.documents):
            raise StopAsyncIteration
        document = self.documents[self.index]
        self.index += 1
        return document

    async def close(self) -> None:
        return None


class FakeCollection:
    """Record the submitted filter and requested limit."""

    def __init__(self, documents: list[dict[str, Any]] | Exception) -> None:
        self.documents = documents
        self.filter: dict[str, Any] | None = None
        self.limit = 0

    def find(self, filter: dict[str, Any], *, limit: int) -> FakeCursor:
        self.filter = filter
        self.limit = limit
        return FakeCursor(self.documents)


class FakeDriver:
    """Expose one scripted collection through the driver's native shape."""

    def __init__(self, collection: FakeCollection) -> None:
        self.collection = collection

    async def __aenter__(self) -> "FakeDriver":
        return self

    async def __aexit__(self, *_args: object) -> None:
        return None

    def __getitem__(self, _name: str) -> "FakeDriver | FakeCollection":
        return self.collection if _name == "orders" else self


def _call() -> Call:
    reference = ConnectionRef("mongodb", "mongodb")
    call_id = CallID("mongodb-python-test")
    return Call(call_id, reference, OperationRef("mongodb", "findDocuments"), IdempotencyKey(call_id))


def _client(collection: FakeCollection, max_documents: int = 100) -> Client:
    return Client(
        Config(database="example", max_documents=max_documents), CredentialSource(),
        create_driver=lambda *_args, **_kwargs: FakeDriver(collection),  # type: ignore[arg-type]
    )


@pytest.mark.asyncio
async def test_canonical_extended_json_and_native_find_limit() -> None:
    """Preserve Go's BSON wrapper shape and fetch one extra document."""
    collection = FakeCollection([{"_id": ObjectId("507f1f77bcf86cd799439011"), "count": 3}])
    result = await _client(collection).find_documents().invoke(
        _call(), FindDocumentsInput("orders", {"status": "active"})
    )
    assert result.branch == "found"
    assert result.value is not None
    assert result.value.documents == ({"_id": {"$oid": "507f1f77bcf86cd799439011"},
                                       "count": {"$numberInt": "3"}},)
    assert collection.filter == {"status": "active"}
    assert collection.limit == 101


@pytest.mark.asyncio
async def test_truncation_and_no_javascript_operator() -> None:
    """Return only configured documents and reject JavaScript before I/O."""
    collection = FakeCollection([{"status": "first"}, {"status": "second"}])
    client = _client(collection, max_documents=1)
    truncated = await client.find_documents().invoke(_call(), FindDocumentsInput("orders", {}))
    assert truncated.branch == "truncated"
    assert truncated.value is not None
    assert truncated.value.documents == ({"status": "first"},)
    rejected = await client.find_documents().invoke(
        _call(), FindDocumentsInput("orders", {"$where": "return true"})
    )
    assert rejected.branch == "defect"


@pytest.mark.asyncio
async def test_rejection_and_retry_classification() -> None:
    """Conclusive command rejection branches; transient read failures retry."""
    rejected_client = _client(FakeCollection(OperationFailure("unauthorized", code=13)))
    rejected = await rejected_client.find_documents().invoke(
        _call(), FindDocumentsInput("orders", {})
    )
    assert rejected.branch == "providerRejected"
    unavailable = await _client(FakeCollection(ConnectionFailure("connection lost"))).find_documents().invoke(
        _call(), FindDocumentsInput("orders", {})
    )
    assert unavailable.kind == "retry"


@pytest.mark.asyncio
async def test_invalid_bson_and_response_byte_limit() -> None:
    """Classify malformed wire values and preserve the prefix that fits."""
    invalid = await _client(FakeCollection(InvalidBSON("bad wire document"))).find_documents().invoke(
        _call(), FindDocumentsInput("orders", {})
    )
    assert invalid.branch == "invalidResponse"
    small = Client(
        Config(database="example", max_response_bytes=8), CredentialSource(),
        create_driver=lambda *_args, **_kwargs: FakeDriver(FakeCollection([{"status": "active"}])),  # type: ignore[arg-type]
    )
    truncated = await small.find_documents().invoke(_call(), FindDocumentsInput("orders", {}))
    assert truncated.branch == "truncated"
    assert truncated.value is not None and truncated.value.documents == ()


@pytest.mark.asyncio
@pytest.mark.parametrize("lookup_error", [Timeout, NXDOMAIN])
async def test_srv_dns_failure_retries(
    monkeypatch: pytest.MonkeyPatch, lookup_error: type[DNSException]
) -> None:
    """SRV lookups happen while connecting, so DNS failures retry instead of selecting defect."""

    async def failing_resolve(*_args: object, **_kwargs: object) -> object:
        raise lookup_error

    monkeypatch.setattr(srv_resolver, "_resolve", failing_resolve)
    client = Client(
        Config(database="example"), CredentialSource("mongodb+srv://reader:secret@cluster.example.com/example")
    )
    result = await client.find_documents().invoke(_call(), FindDocumentsInput("orders", {}))
    assert result.kind == "retry"
    assert result.failure is not None and result.failure.message == "MongoDB DNS lookup failed"


@pytest.mark.asyncio
async def test_malformed_connection_uri_selects_defect() -> None:
    """A URI the native driver cannot parse is a configuration defect."""
    client = Client(Config(database="example"), CredentialSource("mongodb://reader:secret@localhost:notaport"))
    result = await client.find_documents().invoke(_call(), FindDocumentsInput("orders", {}))
    assert result.branch == "defect"
