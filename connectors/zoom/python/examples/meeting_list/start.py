# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start the Zoom meeting-list Flow with a unique Flow ID."""

import argparse
import asyncio
import os
import tempfile

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.zoom import ListMeetingsInput, new_project_connection

from .flow import CONNECTION_NAME, MeetingListFlow


async def start_meeting_list(flow_id: str, page_size: int = 30) -> str:
    """Submit one page request to Dex."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = MeetingListFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="zoom-python-start-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 1 << 24))
            try:
                address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=address)
                ) as client:
                    return await client.start_flow(
                        flow, flow_id, ListMeetingsInput(type="upcoming", page_size=page_size)
                    )
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Accept a unique Flow ID and an optional page size."""
    parser = argparse.ArgumentParser(description="List Zoom meetings")
    parser.add_argument("flow_id")
    parser.add_argument("--page-size", type=int, default=30)
    arguments = parser.parse_args()
    print(asyncio.run(start_meeting_list(arguments.flow_id, arguments.page_size)))


if __name__ == "__main__":
    main()
