# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one Teams thread reply Flow."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.microsoft_teams import new_project_connection

from .flow import CONNECTION_NAME, ThreadRepliesFlow, ThreadRequest


async def start_thread_read(flow_id: str, input_file: Path) -> str:
    """Validate one bounded input and start the Flow."""
    with input_file.open("rb") as source:
        encoded = source.read(65_537)
    if len(encoded) > 65_536:
        raise ValueError("Teams input exceeds 65536 bytes")
    input_value = (
        dex.CodecRegistry()
        .resolve(ThreadRequest)
        .decode(dex.Value(dex.WireKind.JSON, encoded.decode("utf-8")))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = ThreadRepliesFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="teams-python-start-blobs-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
            try:
                async with dex.AsyncClient(
                    registry,
                    cache,
                    dex.ClientOptions(
                        server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                    ),
                ) as client:
                    return await client.start_flow(flow, flow_id, input_value)
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Accept a unique Flow ID and JSON input file."""
    parser = argparse.ArgumentParser(description="Read a Microsoft Teams thread")
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    arguments = parser.parse_args()
    print(asyncio.run(start_thread_read(arguments.flow_id, arguments.input_file)))


if __name__ == "__main__":
    main()
