# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start a Google Drive text copy Flow from a JSON input file."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.google_drive import new_project_connection

from .flow import CONNECTION_NAME, TextCopyFlow, TextCopyInput


async def start_text_copy(flow_id: str, input_file: Path) -> str:
    """Decode the safe start input and submit it to a registered Dex Worker."""
    encoded_input = input_file.read_bytes()
    if len(encoded_input) > 65_536:
        raise ValueError("text copy input exceeds 65536 bytes")
    request = (
        dex.CodecRegistry()
        .resolve(TextCopyInput)
        .decode(dex.Value(dex.WireKind.JSON, encoded_input.decode("utf-8")))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = TextCopyFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="google-drive-python-start-blobs-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
            try:
                async with dex.AsyncClient(
                    registry,
                    cache,
                    dex.ClientOptions(
                        server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                    ),
                ) as client:
                    return await client.start_flow(flow, flow_id, request)
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Accept a unique Flow ID and JSON input path."""
    parser = argparse.ArgumentParser(description="Start a Google Drive text copy Flow")
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    arguments = parser.parse_args()
    print(asyncio.run(start_text_copy(arguments.flow_id, arguments.input_file)))


if __name__ == "__main__":
    main()
