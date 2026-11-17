# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one Easemob user detail Flow with an empty input."""

import argparse
import asyncio
import os
import tempfile

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.easemob import GetUserDetailInput, new_project_connection

from .flow import CONNECTION_NAME, UserReadFlow


async def start_user_read(flow_id: str, user_id: str) -> str:
    """Start a uniquely identified user detail Flow against the configured Dex Server."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = UserReadFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="easemob-user_read-start-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
            try:
                address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=address)
                ) as client:
                    return await client.start_flow(flow, flow_id, GetUserDetailInput(user_id))
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Accept one unique Flow ID and start a user detail lookup."""
    parser = argparse.ArgumentParser(description="Start an Easemob user detail Flow")
    parser.add_argument("flow_id")
    parser.add_argument("user_id")
    arguments = parser.parse_args()
    print(asyncio.run(start_user_read(arguments.flow_id, arguments.user_id)))


if __name__ == "__main__":
    main()
