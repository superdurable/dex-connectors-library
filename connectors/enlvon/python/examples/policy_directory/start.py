# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one Enlvon knowledge-base directory Flow with an empty input."""

import argparse
import asyncio
import os
import tempfile

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.enlvon import SearchPoliciesInput, new_project_connection

from .flow import CONNECTION_NAME, PolicyDirectoryFlow


async def start_policy_directory(flow_id: str, query: str, top_k: int = 0) -> str:
    """Start a uniquely identified knowledge-base directory Flow against the configured Dex Server."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = PolicyDirectoryFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="enlvon-policy_directory-start-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
            try:
                address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=address)
                ) as client:
                    return await client.start_flow(flow, flow_id, SearchPoliciesInput(query, top_k))
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Accept one unique Flow ID and start a knowledge-base directory lookup."""
    parser = argparse.ArgumentParser(description="Start a Enlvon Account knowledge-base directory Flow")
    parser.add_argument("flow_id")
    parser.add_argument("query")
    parser.add_argument("--top-k", type=int, default=0)
    arguments = parser.parse_args()
    print(asyncio.run(start_policy_directory(arguments.flow_id, arguments.query, arguments.top_k)))


if __name__ == "__main__":
    main()
