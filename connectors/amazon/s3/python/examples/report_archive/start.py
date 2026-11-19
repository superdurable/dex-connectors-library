# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start a report archive Flow using safe JSON input."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.amazon_s3 import new_project_connection

from .flow import CONNECTION_NAME, ReportArchiveFlow, ReportArchiveInput


async def start_report_archive(flow_id: str, input_file: Path) -> str:
    """Decode one bounded start value and submit the registered Flow."""
    encoded = input_file.read_bytes()
    if len(encoded) > 65_536:
        raise ValueError("report archive input exceeds 65536 bytes")
    input_value = (
        dex.CodecRegistry()
        .resolve(ReportArchiveInput)
        .decode(dex.Value(dex.WireKind.JSON, encoded.decode("utf-8")))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = ReportArchiveFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="s3-python-start-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 1 << 24))
            try:
                address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=address)
                ) as client:
                    return await client.start_flow(flow, flow_id, input_value)
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Accept a unique Flow ID and JSON input path from the command line."""
    parser = argparse.ArgumentParser(description="Start an S3 report archive Flow")
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    args = parser.parse_args()
    print(asyncio.run(start_report_archive(args.flow_id, args.input_file)))


if __name__ == "__main__":
    main()
