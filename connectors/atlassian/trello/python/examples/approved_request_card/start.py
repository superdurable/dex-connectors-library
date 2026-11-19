# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one Trello card read with the Python Dex Client."""

import argparse
import asyncio
import os
import tempfile

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.atlassian_trello import GetCardInput, new_project_connection

from .flow import CONNECTION_NAME, ApprovedRequestCardFlow


async def start_card_read(flow_id: str, card_id: str) -> str:
    """Start a unique Flow for a Trello card ID."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = ApprovedRequestCardFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="trello-python-start-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
            try:
                async with dex.AsyncClient(
                    registry,
                    cache,
                    dex.ClientOptions(
                        server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:22100")
                    ),
                ) as client:
                    return await client.start_flow(flow, flow_id, GetCardInput(card_id))
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Accept one unique Flow ID and Trello card ID."""
    parser = argparse.ArgumentParser(description="Read one Trello card through Dex")
    parser.add_argument("flow_id")
    parser.add_argument("card_id")
    arguments = parser.parse_args()
    print(asyncio.run(start_card_read(arguments.flow_id, arguments.card_id)))


if __name__ == "__main__":
    main()
