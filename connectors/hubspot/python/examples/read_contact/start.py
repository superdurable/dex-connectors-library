# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one HubSpot contact Flow from a JSON input file."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.hubspot import GetObjectInput, new_project_connection

from .flow import CONNECTION_NAME, ReadContactFlow


async def start_contact(flow_id: str, input_file: Path) -> str:
    """Decode the safe start input and submit a unique Flow ID."""
    encoded = input_file.read_bytes()
    if len(encoded) > 65536:
        raise ValueError("contact input exceeds 65536 bytes")
    value = dex.CodecRegistry().resolve(GetObjectInput).decode(dex.Value(dex.WireKind.JSON, encoded.decode()))
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = ReadContactFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="hubspot-python-start-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 1 << 24))
            try:
                async with dex.AsyncClient(
                    registry,
                    cache,
                    dex.ClientOptions(
                        server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                    ),
                ) as client:
                    return await client.start_flow(flow, flow_id, value)
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Accept a Flow ID and JSON path from the command line."""
    parser = argparse.ArgumentParser(description="Start a HubSpot contact Flow")
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    args = parser.parse_args()
    print(asyncio.run(start_contact(args.flow_id, args.input_file)))


if __name__ == "__main__":
    main()
