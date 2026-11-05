# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start an OAuth HTTP request Flow with a bounded JSON input file."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.superdurable_oauth2_http import SendRequestInput, new_project_connection

from .flow import CONNECTION_NAME, APIRequestFlow


async def start_api_request(flow_id: str, input_file: Path) -> str:
    """Decode a request and submit it to Dex."""
    encoded = input_file.read_bytes()
    if len(encoded) > 65_536:
        raise ValueError("OAuth HTTP request input exceeds 65536 bytes")
    request = (
        dex.CodecRegistry()
        .resolve(SendRequestInput)
        .decode(dex.Value(dex.WireKind.JSON, encoded.decode("utf-8")))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = APIRequestFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="oauth2-http-python-start-") as cache_directory:
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
    """Read a unique Flow ID and request file from the command line."""
    parser = argparse.ArgumentParser(description="Start an OAuth HTTP request Flow")
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    arguments = parser.parse_args()
    print(asyncio.run(start_api_request(arguments.flow_id, arguments.input_file)))


if __name__ == "__main__":
    main()
