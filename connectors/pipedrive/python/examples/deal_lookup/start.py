# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start a Pipedrive deal lookup against the local Dex service."""

import argparse
import asyncio
import os
import tempfile

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.pipedrive import GetObjectInput, new_project_connection

from .flow import CONNECTION_NAME, DealLookupFlow


async def start_deal_lookup(flow_id: str, deal_id: str) -> str:
    """Submit a deal ID to the registered Flow."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = DealLookupFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="pipedrive-python-start-blobs-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
            try:
                async with dex.AsyncClient(
                    registry,
                    cache,
                    dex.ClientOptions(
                        server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                    ),
                ) as client:
                    return await client.start_flow(flow, flow_id, GetObjectInput("deals", deal_id))
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Accept a unique Flow ID and decimal deal ID."""
    parser = argparse.ArgumentParser(description="Start a Pipedrive deal lookup")
    parser.add_argument("flow_id")
    parser.add_argument("deal_id")
    arguments = parser.parse_args()
    print(asyncio.run(start_deal_lookup(arguments.flow_id, arguments.deal_id)))


if __name__ == "__main__":
    main()
