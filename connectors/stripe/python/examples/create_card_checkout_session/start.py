# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start the Stripe card Checkout example while Python FDG support is pending."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.stripe import new_project_connection

from .flow import CONNECTION_NAME, CardCheckoutFlow, CardCheckoutInput


async def start_card_checkout(flow_id: str, input_file: Path) -> str:
    """Decode one safe start input and submit it to the registered Dex Flow."""
    with input_file.open("rb") as source:
        encoded_input = source.read(65_537)
    if len(encoded_input) > 65_536:
        raise ValueError("card Checkout input exceeds 65536 bytes")
    input_value = dex.CodecRegistry().resolve(CardCheckoutInput).decode(
        dex.Value(dex.WireKind.JSON, encoded_input.decode("utf-8"))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = CardCheckoutFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="stripe-python-start-blobs-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
            try:
                server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=server_address)
                ) as client:
                    return await client.start_flow(flow, flow_id, input_value)
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Accept a unique Flow ID and JSON input path from the command line."""
    parser = argparse.ArgumentParser(description="Start a Stripe card Checkout Flow")
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    arguments = parser.parse_args()
    run_id = asyncio.run(start_card_checkout(arguments.flow_id, arguments.input_file))
    print(run_id)


if __name__ == "__main__":
    main()
