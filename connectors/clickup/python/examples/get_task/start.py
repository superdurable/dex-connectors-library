# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one ClickUp task-read Flow."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.clickup import new_project_connection

from .flow import CONNECTION_NAME, GetTaskFlow, GetTaskRequest


async def start_task_read(flow_id: str, input_file: Path) -> str:
    """Decode one safe task ID and start its Flow."""
    with input_file.open("rb") as source:
        encoded = source.read(65_537)
    if len(encoded) > 65_536:
        raise ValueError("ClickUp input exceeds 65536 bytes")
    input_value = (
        dex.CodecRegistry()
        .resolve(GetTaskRequest)
        .decode(dex.Value(dex.WireKind.JSON, encoded.decode("utf-8")))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = GetTaskFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="clickup-python-start-blobs-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
            try:
                server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=server_address)
                ) as client:
                    return await client.start_flow(flow, flow_id, input_value)
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Take a unique Flow ID and JSON input path."""
    parser = argparse.ArgumentParser(description="Start a ClickUp task read")
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    arguments = parser.parse_args()
    print(asyncio.run(start_task_read(arguments.flow_id, arguments.input_file)))


if __name__ == "__main__":
    main()
