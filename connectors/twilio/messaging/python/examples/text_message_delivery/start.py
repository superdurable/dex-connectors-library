# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one Twilio message delivery Flow from a JSON input file."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.twilio_messaging import SendMessageInput, new_project_connection

from .flow import CONNECTION_NAME, MessageDeliveryFlow


async def start_message_delivery(flow_id: str, input_file: Path) -> str:
    """Decode a bounded input file and start the registered Flow."""
    encoded = input_file.read_bytes()
    if len(encoded) > 65536:
        raise ValueError("Twilio message input exceeds 65536 bytes")
    input_value = (
        dex.CodecRegistry()
        .resolve(SendMessageInput)
        .decode(dex.Value(dex.WireKind.JSON, encoded.decode("utf-8")))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    flow = MessageDeliveryFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    try:
        with tempfile.TemporaryDirectory(prefix="twilio-python-start-blobs-") as directory:
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
    """Accept a unique Flow ID and JSON input path."""
    parser = argparse.ArgumentParser(description="Start one Twilio message delivery Flow")
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    arguments = parser.parse_args()
    print(asyncio.run(start_message_delivery(arguments.flow_id, arguments.input_file)))


if __name__ == "__main__":
    main()
