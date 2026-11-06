# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start a policy read through the static Google Docs connection."""

import argparse
import asyncio
import os
import tempfile

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.google_docs import new_project_connection

from .flow import CONNECTION_NAME, PolicyReadFlow, PolicyReadRequest


async def start_policy_read(flow_id: str, document_id: str) -> str:
    """Start one Flow for a Google Docs document ID."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = PolicyReadFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="google-docs-python-start-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 1 << 24))
            try:
                async with dex.AsyncClient(
                    registry, cache,
                    dex.ClientOptions(
                        server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                    ),
                ) as client:
                    return await client.start_flow(flow, flow_id, PolicyReadRequest(document_id))
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Parse a unique Flow ID and document ID."""
    parser = argparse.ArgumentParser(description="Read a Google Doc through Dex")
    parser.add_argument("flow_id")
    parser.add_argument("document_id")
    arguments = parser.parse_args()
    print(asyncio.run(start_policy_read(arguments.flow_id, arguments.document_id)))


if __name__ == "__main__":
    main()
