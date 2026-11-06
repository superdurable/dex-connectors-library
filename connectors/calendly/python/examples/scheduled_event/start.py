# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Submit a scheduled event lookup to Dex."""

import argparse
import asyncio
import os
import tempfile

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.calendly import GetScheduledEventInput, new_project_connection

from .flow import CONNECTION_NAME, ScheduledEventFlow


async def start_lookup(flow_id: str, scheduled_event_uri: str) -> str:
    """Start a lookup with a unique Flow ID and stable Calendly event URI."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = ScheduledEventFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="calendly-python-start-blobs-") as cache_path:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_path, 1 << 24))
            try:
                async with dex.AsyncClient(
                    registry,
                    cache,
                    dex.ClientOptions(
                        server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                    ),
                ) as client:
                    return await client.start_flow(flow, flow_id, GetScheduledEventInput(scheduled_event_uri))
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Accept a Flow ID and Calendly event URI from the command line."""
    parser = argparse.ArgumentParser(description="Start a Calendly event lookup")
    parser.add_argument("flow_id")
    parser.add_argument("scheduled_event_uri")
    options = parser.parse_args()
    print(asyncio.run(start_lookup(options.flow_id, options.scheduled_event_uri)))


if __name__ == "__main__":
    main()
