# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one Jiandaoyun application page Flow."""

import argparse
import asyncio
import os
import tempfile

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.fanruan import ListAppsPageInput, new_project_connection

from .flow import CONNECTION_NAME, AppDirectoryFlow


async def start_app_directory(flow_id: str, skip: int = 0, limit: int = 0) -> str:
    """Start an application page Flow against the configured Dex Server."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = AppDirectoryFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="fanruan-app-directory-start-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
            try:
                address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=address)
                ) as client:
                    return await client.start_flow(flow, flow_id, ListAppsPageInput(skip, limit))
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Accept a unique Flow ID and page controls."""
    parser = argparse.ArgumentParser(description="Start a Jiandaoyun application directory Flow")
    parser.add_argument("flow_id")
    parser.add_argument("--skip", type=int, default=0)
    parser.add_argument("--limit", type=int, default=0)
    arguments = parser.parse_args()
    print(asyncio.run(start_app_directory(arguments.flow_id, arguments.skip, arguments.limit)))


if __name__ == "__main__":
    main()
