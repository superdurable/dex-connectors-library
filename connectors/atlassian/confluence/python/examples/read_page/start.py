# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start the Confluence read example from a JSON file."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.atlassian_confluence import new_project_connection

from .flow import CONNECTION_NAME, ReadPageFlow, ReadPageRequest


async def start_read_page(flow_id: str, source: Path) -> str:
    """Start one page read with a unique Flow ID."""
    encoded = source.read_bytes()
    if len(encoded) > 65536:
        raise ValueError("read request exceeds 65536 bytes")
    request = (
        dex.CodecRegistry().resolve(ReadPageRequest).decode(dex.Value(dex.WireKind.JSON, encoded.decode()))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = ReadPageFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="confluence-read-start-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 1 << 24))
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
    """Parse a Flow ID and input path."""
    parser = argparse.ArgumentParser()
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    args = parser.parse_args()
    print(asyncio.run(start_read_page(args.flow_id, args.input_file)))


if __name__ == "__main__":
    main()
