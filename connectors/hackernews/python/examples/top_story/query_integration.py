# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run a named Hacker News query Flow through a real Dex Worker and Client."""

import asyncio
import os
from collections.abc import Callable
from datetime import timedelta
from tempfile import TemporaryDirectory
from typing import TypeVar
from uuid import uuid4

import dex
from dex_connectors.sdk import ConnectionRef

from dex_connectors.hackernews import Client, Connection, new_connection

Request = TypeVar("Request")
Output = TypeVar("Output")


async def read_once(
    provider: Client,
    name: str,
    create_flow: Callable[[Connection], dex.Flow[Request]],
    request: Request,
    output_type: type[Output],
) -> Output:
    """Run one Flow and return its decoded result after Worker shutdown."""
    connection = new_connection(provider, ConnectionRef("hacker-news", name))
    flow = create_flow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
    try:
        with TemporaryDirectory() as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 1 << 24))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
            serving = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=15)
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=address)
                ) as client:
                    flow_id = f"{flow.get_flow_type()}-{uuid4().hex}"
                    await client.start_flow(flow, flow_id, request)
                    result = await client.wait_for_flow(flow_id, timedelta(seconds=30))
                    if result.status != dex.FlowStatus.COMPLETED:
                        raise RuntimeError(f"{flow.get_flow_type()} failed: {result.error_message}")
                    return result.single_output(output_type)
            finally:
                await worker.stop()
                await serving
                cache.close()
    finally:
        await connection.aclose()
