# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real native MongoDB protocol scenarios selected by the CI service job."""

import os
from typing import Any
from uuid import uuid4

import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, IdempotencyKey, OperationRef, SecretString
from pymongo import AsyncMongoClient

from dex_connectors.mongodb import Client, Config, Credentials, FindDocumentsInput


class CredentialSource:
    """Resolve the CI service URI per attempt."""

    async def resolve(self, call: Call) -> Credentials:
        return Credentials(SecretString(os.environ["MONGODB_CONNECTOR_TEST_URI"]))


def _call() -> Call:
    reference = ConnectionRef("mongodb", "native-test")
    call_id = CallID(uuid4().hex)
    return Call(call_id, reference, OperationRef("mongodb", "findDocuments"), IdempotencyKey(call_id))


@pytest.mark.asyncio
async def test_real_document_read_and_count_limit() -> None:
    """Read BSON through the native driver and classify one extra match."""
    uri = os.environ.get("MONGODB_CONNECTOR_TEST_URI")
    if not uri:
        pytest.skip("real MongoDB service is configured in CI only")
    collection_name = "connector_python_" + uuid4().hex
    native_client: AsyncMongoClient[dict[str, Any]] = AsyncMongoClient(uri)
    async with native_client as provider_client:
        collection = provider_client["connector_test"][collection_name]
        await collection.insert_many([{"status": "active", "count": 3}, {"status": "active", "count": 4}])
        try:
            connector = Client(Config(database="connector_test", max_documents=1), CredentialSource())
            outcome = await connector.find_documents().invoke(
                _call(), FindDocumentsInput(collection_name, {"status": "active"})
            )
            assert outcome.branch == "truncated"
            assert outcome.value is not None and len(outcome.value.documents) == 1
            assert outcome.value.documents[0] is not None
            assert outcome.value.documents[0]["count"] in ({"$numberInt": "3"}, {"$numberInt": "4"})
            empty = await connector.find_documents().invoke(
                _call(), FindDocumentsInput(collection_name, {"status": "missing"})
            )
            assert empty.branch == "found"
            assert empty.value is not None and empty.value.documents == ()
        finally:
            await collection.drop()
