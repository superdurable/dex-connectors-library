# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start a Snowflake Python Flow using the configured connection."""

import argparse
import asyncio
import json
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.snowflake import CancelStatementInput, SubmitStatementInput, new_project_connection

from .cancel_flow import CancelStatementFlow
from .flow import CONNECTION_NAME, StatementResultFlow


async def start_flow(flow_id: str, input_file: Path, cancel: bool = False) -> str:
    """Decode a bounded JSON input and start the chosen registered Flow."""
    encoded = input_file.read_bytes()
    if len(encoded) > 65536:
        raise ValueError("Snowflake Flow input exceeds 65536 bytes")
    document = json.loads(encoded)
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        registry = dex.Registry(
            [StatementResultFlow(connection), CancelStatementFlow(connection)], allow_async_handlers=True
        )
        with tempfile.TemporaryDirectory(prefix="snowflake-python-start-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
            try:
                address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=address)
                ) as client:
                    if cancel:
                        return await client.start_flow(
                            CancelStatementFlow(connection),
                            flow_id,
                            CancelStatementInput(statement_handle=document["statementHandle"]),
                        )
                    return await client.start_flow(
                        StatementResultFlow(connection),
                        flow_id,
                        SubmitStatementInput(
                            statement=document["statement"], parameters=tuple(document.get("parameters", ()))
                        ),
                    )
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Parse a unique Flow ID and JSON input file."""
    parser = argparse.ArgumentParser(description="Start a Snowflake Python Flow")
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    parser.add_argument("--cancel", action="store_true")
    arguments = parser.parse_args()
    print(asyncio.run(start_flow(arguments.flow_id, arguments.input_file, arguments.cancel)))


if __name__ == "__main__":
    main()
