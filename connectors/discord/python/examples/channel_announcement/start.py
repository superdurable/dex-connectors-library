# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start a Discord announcement Flow from a JSON input file."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.discord import new_project_connection

from .flow import CONNECTION_NAME, AnnouncementInput, ChannelAnnouncementFlow


async def start_announcement(flow_id: str, input_file: Path) -> str:
    """Decode the safe start input and submit it to Dex."""
    encoded = input_file.read_bytes()
    if len(encoded) > 65_536:
        raise ValueError("announcement input exceeds 65536 bytes")
    input_value = (
        dex.CodecRegistry().resolve(AnnouncementInput).decode(dex.Value(dex.WireKind.JSON, encoded.decode()))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = ChannelAnnouncementFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="discord-python-start-blobs-") as cache_directory:
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
    """Accept a unique Flow ID and JSON input path."""
    parser = argparse.ArgumentParser(description="Start a Discord announcement Flow")
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    args = parser.parse_args()
    print(asyncio.run(start_announcement(args.flow_id, args.input_file)))


if __name__ == "__main__":
    main()
