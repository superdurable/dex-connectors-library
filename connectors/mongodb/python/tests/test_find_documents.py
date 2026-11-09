# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Native driver branch, retry, and Extended JSON agreement tests."""

from typing import Any

import pytest
from bson import ObjectId
from bson.errors import InvalidBSON
from dex_connectors.sdk import Call, CallID, ConnectionRef, IdempotencyKey, OperationRef, SecretString
from pymongo.errors import ConnectionFailure, OperationFailure

from dex_connectors.mongodb import Client, Config, Credentials, FindDocumentsInput


class CredentialSource:
    """Return a synthetic URI without storing it in Flow state."""

    async def resolve(self, call: Call) -> Credentials:
        return Credentials(SecretString("mongodb://reader:secret@localhost:27017"))


class FakeCursor:
    """Return scripted native driver documents or a scripted failure."""

    def __init__(self, documents: list[dict[str, Any]] | Exception) -> None:
        self.documents = documents

    async def to_list(self, length: int) -> list[dict[str, Any]]:
        if isinstance(self.documents, Exception):
            raise self.documents
        return self.documents[:length]


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
