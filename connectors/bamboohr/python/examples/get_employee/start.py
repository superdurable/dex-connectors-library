# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start a BambooHR employee lookup Flow from a bounded JSON file."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.bamboohr import GetEmployeeInput, new_project_connection

from .flow import CONNECTION_NAME, GetEmployeeFlow


async def start_employee_lookup(flow_id: str, input_file: Path) -> str:
    """Decode one employee request and submit it to Dex."""
    with input_file.open("rb") as source:
        encoded = source.read(65537)
    if len(encoded) > 65536:
        raise ValueError("employee lookup input exceeds 65536 bytes")
    request = (
        dex.CodecRegistry().resolve(GetEmployeeInput).decode(dex.Value(dex.WireKind.JSON, encoded.decode()))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = GetEmployeeFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="bamboohr-python-start-") as directory:
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
    """Accept a unique Flow ID and an employee request JSON path."""
    parser = argparse.ArgumentParser(description="Start a BambooHR employee lookup")
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    arguments = parser.parse_args()
    print(asyncio.run(start_employee_lookup(arguments.flow_id, arguments.input_file)))


if __name__ == "__main__":
    main()
