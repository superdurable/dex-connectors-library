# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start a policy lookup using the static Airtable project connection."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.airtable import new_project_connection

from .flow import CONNECTION_NAME, PolicyLookupFlow, PolicyLookupRequest


async def start_policy_lookup(flow_id: str, input_file: Path) -> str:
    """Decode one bounded JSON input and start a policy lookup Flow."""
    with input_file.open("rb") as source:
        encoded_input = source.read(65_537)
    if len(encoded_input) > 65_536:
        raise ValueError("policy lookup input exceeds 65536 bytes")
    request = (
        dex.CodecRegistry()
        .resolve(PolicyLookupRequest)
        .decode(dex.Value(dex.WireKind.JSON, encoded_input.decode("utf-8")))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = PolicyLookupFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="airtable-python-start-blobs-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
            try:
                server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=server_address)
                ) as client:
                    return await client.start_flow(flow, flow_id, request)
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Read the unique Flow ID and JSON input path from the command line."""
    parser = argparse.ArgumentParser(description="Start an Airtable policy lookup Flow")
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    arguments = parser.parse_args()
    print(asyncio.run(start_policy_lookup(arguments.flow_id, arguments.input_file)))


if __name__ == "__main__":
    main()
