# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one Chuanglan balance Flow with an empty input."""

import argparse
import asyncio
import os
import tempfile

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.chuanglan import GetBalanceInput, new_project_connection

from .flow import CONNECTION_NAME, BalanceFlow


async def start_balance(flow_id: str) -> str:
    """Start a uniquely identified balance Flow against the configured Dex Server."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = BalanceFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="chuanglan-balance-start-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
            try:
                address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=address)
                ) as client:
                    return await client.start_flow(flow, flow_id, GetBalanceInput())
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Accept one unique Flow ID and start a balance lookup."""
    parser = argparse.ArgumentParser(description="Start a Chuanglan SMS balance Flow")
    parser.add_argument("flow_id")
    arguments = parser.parse_args()
    print(asyncio.run(start_balance(arguments.flow_id)))


if __name__ == "__main__":
    main()
