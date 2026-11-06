# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one Zoho Desk ticket search Flow through Dex."""

import asyncio
import json
import os
import tempfile
from pathlib import Path
from uuid import uuid4

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.zoho_desk import SearchTicketsInput, new_project_connection

from .flow import CONNECTION_NAME, SearchTicketsFlow


async def run() -> None:
    """Start a search from a JSON input file and print its Flow ID."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = SearchTicketsFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(
            dex.BlobCacheConfig(
                str(Path(tempfile.gettempdir()) / "dex-zoho-desk-helpdesk-search-client"), 1 << 30
            )
        )
        try:
            raw = json.loads(
                Path(
                    os.environ.get("ZOHO_DESK_INPUT_FILE", "examples/search_tickets/input.example.json")
                ).read_text()
            )
            request = SearchTicketsInput(**raw)
            async with dex.AsyncClient(
                registry,
                cache,
                dex.ClientOptions(
                    server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                ),
            ) as client:
                flow_id = f"zoho-desk-helpdesk-search-{uuid4()}"
                await client.start_flow(flow, flow_id, request)
                print(flow_id)
        finally:
            cache.close()
    finally:
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(run())
