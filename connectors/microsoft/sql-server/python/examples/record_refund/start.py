# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start the checked-in SQL Server refund Flow from JSON input."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.microsoft_sql_server import new_project_connection

from .flow import CONNECTION_NAME, RecordRefundFlow, RefundRequest


async def start_refund(flow_id: str, input_file: Path) -> str:
    """Submit one validated JSON request to Dex."""
    encoded = input_file.read_bytes()
    if len(encoded) > 65_536:
        raise ValueError("refund input exceeds 65536 bytes")
    request = (
        dex.CodecRegistry()
        .resolve(RefundRequest)
        .decode(dex.Value(dex.WireKind.JSON, encoded.decode("utf-8")))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = RecordRefundFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="sqlserver-python-start-blobs-") as cache_directory:
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
    """Accept a unique Flow ID and request JSON path."""
    parser = argparse.ArgumentParser(description="Start a SQL Server refund Flow")
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    args = parser.parse_args()
    print(asyncio.run(start_refund(args.flow_id, args.input_file)))


if __name__ == "__main__":
    main()
