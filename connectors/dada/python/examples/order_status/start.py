# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one Dada order status Flow with a third-party order ID."""

import argparse
import asyncio
import os
import tempfile

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.dada import QueryOrderStatusInput, new_project_connection

from .flow import CONNECTION_NAME, OrderStatusFlow


async def start_order_status(flow_id: str, order_id: str) -> str:
    """Start a uniquely identified order status Flow against the configured Dex Server."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = OrderStatusFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="dada-order_status-start-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
            try:
                address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=address)
                ) as client:
                    return await client.start_flow(flow, flow_id, QueryOrderStatusInput(order_id))
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Accept one unique Flow ID and start a order status lookup."""
    parser = argparse.ArgumentParser(description="Start a Dada Account order status Flow")
    parser.add_argument("flow_id")
    parser.add_argument("order_id")
    arguments = parser.parse_args()
    print(asyncio.run(start_order_status(arguments.flow_id, arguments.order_id)))


if __name__ == "__main__":
    main()
