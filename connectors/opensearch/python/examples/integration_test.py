# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification of OpenSearch Python Flows."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.opensearch import (
    Client,
    Config,
    Credentials,
    DeletedDocument,
    DeleteDocumentInput,
    GetDocumentInput,
    IndexDocumentInput,
    IndexedDocument,
    RetrievedDocument,
    SearchDocumentsInput,
    SearchPage,
    new_connection,
)
from dex_connectors.opensearch.opensearchmock import Mock

from .document_delete.flow import DocumentDeleteFlow
from .document_get.flow import DocumentGetFlow
from .document_index.flow import DocumentIndexFlow
from .document_search.flow import DocumentSearchFlow


@pytest.mark.asyncio
async def test_opensearch_flows_use_generated_mocks_with_real_dex(tmp_path: Path) -> None:
    """Run all checked-in operation Flows through Worker, Client, and Attributes."""
    mock = Mock()
    index_flow = DocumentIndexFlow(mock.connection())
    delete_flow = DocumentDeleteFlow(mock.connection())
    search_flow = DocumentSearchFlow(mock.connection())
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "GET" and request.url.raw_path == b"/products/_doc/sku&%21%2F42"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "_index": "products",
                "_id": "sku&!/42",
                "_version": 3,
                "found": True,
                "_source": {"title": "Meridian"},
            },
        )

    reference = ConnectionRef("opensearch", "opensearch")
    provider = Client(
        Config(cluster_url="http://127.0.0.1:12345"),
        StaticCredentialProvider({reference: Credentials("reader", SecretString("fixture"))}),
        httpx.AsyncClient(transport=httpx.MockTransport(answer)),
    )
    get_flow = DocumentGetFlow(new_connection(provider, reference))
    registry = dex.Registry([index_flow, delete_flow, search_flow, get_flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=15)
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            index_id = f"opensearch-python-index-{uuid4().hex}"
            await client.start_flow(
                index_flow, index_id, IndexDocumentInput("products", "sku-42", {"title": "Meridian"})
            )
            indexed = await asyncio.wait_for(client.wait_for_flow(index_id, timedelta(seconds=25)), 30)
            assert indexed.status == dex.FlowStatus.COMPLETED, indexed.error_message
            assert indexed.single_output(IndexedDocument).id == "sku-42"
            assert len(mock.index_document_script.calls()) == 1

            delete_id = f"opensearch-python-delete-{uuid4().hex}"
            await client.start_flow(delete_flow, delete_id, DeleteDocumentInput("products", "sku-42"))
            deleted = await asyncio.wait_for(client.wait_for_flow(delete_id, timedelta(seconds=25)), 30)
            assert deleted.status == dex.FlowStatus.COMPLETED, deleted.error_message
            assert deleted.single_output(DeletedDocument).was_deleted
            assert len(mock.delete_document_script.calls()) == 1

            search_id = f"opensearch-python-search-{uuid4().hex}"
            await client.start_flow(
                search_flow, search_id, SearchDocumentsInput("products", "title:Meridian")
            )
            searched = await asyncio.wait_for(client.wait_for_flow(search_id, timedelta(seconds=25)), 30)
            assert searched.status == dex.FlowStatus.COMPLETED, searched.error_message
            assert searched.single_output(SearchPage).total == 1
            assert len(mock.search_documents_script.calls()) == 1
            get_id = f"opensearch-python-get-{uuid4().hex}"
            await client.start_flow(get_flow, get_id, GetDocumentInput("products", "sku&!/42"))
            retrieved = await asyncio.wait_for(client.wait_for_flow(get_id, timedelta(seconds=25)), 30)
            assert retrieved.status == dex.FlowStatus.COMPLETED, retrieved.error_message
            assert retrieved.single_output(RetrievedDocument) == RetrievedDocument(
                "products", "sku&!/42", {"title": "Meridian"}, 3
            )
            assert len(requests) == 2
    finally:
        await worker.stop()
        await worker_task
        cache.close()
        await provider.aclose()
