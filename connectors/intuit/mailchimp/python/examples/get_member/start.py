# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start a Mailchimp contact read from a safe JSON input file."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.intuit_mailchimp import GetMemberInput, new_project_connection

from .flow import CONNECTION_NAME, GetMemberFlow


async def start_get_member(flow_id: str, input_file: Path) -> str:
    """Decode a bounded input file and start the registered Flow."""
    encoded = input_file.read_bytes()
    if len(encoded) > 65536:
        raise ValueError("Mailchimp contact input exceeds 65536 bytes")
    input_value = (
        dex.CodecRegistry()
        .resolve(GetMemberInput)
        .decode(dex.Value(dex.WireKind.JSON, encoded.decode("utf-8")))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = GetMemberFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="dex-mailchimp-start-") as cache_directory:
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
    """Accept a unique Flow ID and one input JSON path."""
    parser = argparse.ArgumentParser(description="Read a Mailchimp audience contact")
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    arguments = parser.parse_args()
    print(asyncio.run(start_get_member(arguments.flow_id, arguments.input_file)))


if __name__ == "__main__":
    main()
