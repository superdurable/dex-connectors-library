# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start the ticket triage Flow from a JSON input file."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.gorgias import GetTicketInput, new_project_connection

from .flow import CONNECTION_NAME, TriageIssueFlow


async def start_ticket_triage(flow_id: str, input_path: Path) -> str:
    """Submit one typed ticket request to the configured Dex service."""
    encoded = input_path.read_bytes()
    if len(encoded) > 4096:
        raise ValueError("ticket triage input exceeds 4096 bytes")
    request = (
        dex.CodecRegistry().resolve(GetTicketInput).decode(dex.Value(dex.WireKind.JSON, encoded.decode()))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = TriageIssueFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="gorgias-python-start-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 1 << 24))
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
    """Accept a unique Flow ID and checked-in input path."""
    import sys

    print(asyncio.run(start_ticket_triage(sys.argv[1], Path(sys.argv[2]))))


if __name__ == "__main__":
    main()
