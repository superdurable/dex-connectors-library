# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start a Linear workflow-state Flow with one team UUID."""

import argparse
import asyncio
import os
import tempfile

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.linear import ListWorkflowStatesInput, new_project_connection

from .flow import CONNECTION_NAME, ListWorkflowStatesFlow


async def start_workflow_state_flow(flow_id: str, team_id: str) -> str:
    """Start one Flow using the configured Linear connection."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = ListWorkflowStatesFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="dex-linear-start-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
            try:
                async with dex.AsyncClient(
                    registry,
                    cache,
                    dex.ClientOptions(
                        server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                    ),
                ) as client:
                    return await client.start_flow(flow, flow_id, ListWorkflowStatesInput(team_id))
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Read the Flow ID and team UUID from the command line."""
    parser = argparse.ArgumentParser(description="List Linear workflow states")
    parser.add_argument("flow_id")
    parser.add_argument("team_id")
    arguments = parser.parse_args()
    print(asyncio.run(start_workflow_state_flow(arguments.flow_id, arguments.team_id)))


if __name__ == "__main__":
    main()
