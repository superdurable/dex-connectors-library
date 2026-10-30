# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the checked-in company news Flow against a real local Dex Server."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk import Failure, FailureKind

from dex_connectors.newsapi.newsapimock import (
    Mock,
    search_articles_invalid_key,
    search_articles_retry,
    search_articles_searched_default_answer,
)
from examples.company_news.flow import CompanyNewsFlow
from examples.company_news.request import CompanyNewsOutcome, CompanyNewsRequest


@pytest.mark.asyncio
async def test_company_news_real_dex_worker(tmp_path: Path) -> None:
    """Exercise success, retry, optional rejection, and input failure in Dex."""
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
    mock = Mock()
    flow = CompanyNewsFlow(mock.connection())
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 64 * 1024 * 1024))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(
            registry, cache, dex.ClientOptions(server_address=server_address)
        ) as client:
            success_id = f"newsapi-python-success-{uuid4()}"
            await client.start_flow(flow, success_id, CompanyNewsRequest("Meridian"))
            success = await client.wait_for_flow(success_id, timedelta(seconds=30))
            assert success.status == dex.FlowStatus.COMPLETED
            outputs = [
                item.decode(CompanyNewsOutcome)
                for item in success.completions
                if item.step_type == "CompanyArticlesFound"
            ]
            assert len(outputs) == 1
            assert outputs[0].status == "found"
            assert outputs[0].company == "Meridian"
            assert outputs[0].total_results == 2
            assert len(outputs[0].articles) == 2

            retry_id = f"newsapi-python-retry-{uuid4()}"
            mock.search_articles().for_flow(retry_id).respond(
                search_articles_retry(
                    Failure(FailureKind.AVAILABILITY, "newsapi", "searchArticles", "temporary outage")
                ),
                search_articles_searched_default_answer(),
            )
            await client.start_flow(flow, retry_id, CompanyNewsRequest("Meridian"))
            retried = await client.wait_for_flow(retry_id, timedelta(seconds=30))
            assert retried.status == dex.FlowStatus.COMPLETED
            retry_calls = [call for call in mock.search_articles().calls() if call.flow_id == retry_id]
            assert len(retry_calls) == 2
            assert retry_calls[0].retry
            assert retry_calls[0].call_id == retry_calls[1].call_id

            rejection_id = f"newsapi-python-rejected-{uuid4()}"
            mock.search_articles().for_flow(rejection_id).respond(search_articles_invalid_key())
            await client.start_flow(flow, rejection_id, CompanyNewsRequest("Meridian"))
            rejected = await client.wait_for_flow(rejection_id, timedelta(seconds=30))
            assert rejected.status == dex.FlowStatus.FAILED
            assert rejected.error_message is not None and "providerRejected" in rejected.error_message
            assert len([call for call in mock.search_articles().calls() if call.flow_id == rejection_id]) == 1

            invalid_id = f"newsapi-python-invalid-{uuid4()}"
            await client.start_flow(flow, invalid_id, CompanyNewsRequest(" "))
            invalid = await client.wait_for_flow(invalid_id, timedelta(seconds=30))
            assert invalid.status == dex.FlowStatus.FAILED
            assert len([call for call in mock.search_articles().calls() if call.flow_id == invalid_id]) == 0
        mock.search_articles().assert_no_failures()
    finally:
        await worker.stop()
        await worker_task
        cache.close()
