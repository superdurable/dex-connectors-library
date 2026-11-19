# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one CTYun knowledge-base directory Flow with an empty input."""

import argparse
import asyncio
import os
import tempfile

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.ctyun import ListKnowledgeBasesInput, new_project_connection

from .flow import CONNECTION_NAME, KnowledgeBaseDirectoryFlow


async def start_knowledge_base_directory(flow_id: str) -> str:
    """Start a uniquely identified knowledge-base directory Flow against the configured Dex Server."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = KnowledgeBaseDirectoryFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="ctyun-knowledge_base_directory-start-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
            try:
                address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=address)
                ) as client:
                    return await client.start_flow(flow, flow_id, ListKnowledgeBasesInput())
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Accept one unique Flow ID and start a knowledge-base directory lookup."""
    parser = argparse.ArgumentParser(description="Start a CTYun Account knowledge-base directory Flow")
    parser.add_argument("flow_id")
    arguments = parser.parse_args()
    print(asyncio.run(start_knowledge_base_directory(arguments.flow_id)))


if __name__ == "__main__":
    main()
