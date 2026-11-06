# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start a monday.com work-request Flow from one JSON input file."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.monday import new_project_connection

from .flow import CONNECTION_NAME, WorkRequest, WorkRequestFlow


async def start_work_request(flow_id: str, input_file: Path) -> str:
    """Decode a bounded safe request and submit it to Dex."""
    with input_file.open("rb") as source:
        encoded = source.read(65_537)
    if len(encoded) > 65_536:
        raise ValueError("work request input exceeds 65536 bytes")
    request = (
        dex.CodecRegistry().resolve(WorkRequest).decode(dex.Value(dex.WireKind.JSON, encoded.decode("utf-8")))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = WorkRequestFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="monday-python-start-blobs-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
            try:
                address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=address)
                ) as client:
                    return await client.start_flow(flow, flow_id, request)
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Accept a unique Flow ID and one work request JSON path."""
    parser = argparse.ArgumentParser(description="Start a monday.com work request Flow")
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    arguments = parser.parse_args()
    print(asyncio.run(start_work_request(arguments.flow_id, arguments.input_file)))


if __name__ == "__main__":
    main()
