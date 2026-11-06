# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start a summary Flow with a JSON input file."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.superdurable_llm import new_project_connection

from .flow import CONNECTION_NAME, SummarizeTextFlow, SummarizeTextInput


async def start_summary(flow_id: str, input_file: Path) -> str:
    """Decode a small JSON input and submit it to the registered Flow."""
    encoded = input_file.read_bytes()
    if len(encoded) > 65536:
        raise ValueError("summary input exceeds 65536 bytes")
    input_value = (
        dex.CodecRegistry()
        .resolve(SummarizeTextInput)
        .decode(dex.Value(dex.WireKind.JSON, encoded.decode("utf-8")))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = SummarizeTextFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="llm-python-start-blobs-") as cache_directory:
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
    """Read a Flow ID and sample input path from the command line."""
    parser = argparse.ArgumentParser()
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    arguments = parser.parse_args()
    print(asyncio.run(start_summary(arguments.flow_id, arguments.input_file)))


if __name__ == "__main__":
    main()
