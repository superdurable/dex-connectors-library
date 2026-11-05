# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start the DocuSign envelope status Flow with a unique Flow ID."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.docusign import GetEnvelopeInput, new_project_connection

from .flow import CONNECTION_NAME, EnvelopeStatusFlow


async def start_envelope_status(flow_id: str, input_file: Path) -> str:
    """Decode one safe envelope ID and start the registered Dex Flow."""
    encoded_input = input_file.read_bytes()
    if len(encoded_input) > 4096:
        raise ValueError("envelope status input exceeds 4096 bytes")
    value = (
        dex.CodecRegistry()
        .resolve(GetEnvelopeInput)
        .decode(dex.Value(dex.WireKind.JSON, encoded_input.decode("utf-8")))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = EnvelopeStatusFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="docusign-python-start-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 1 << 24))
            try:
                address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=address)
                ) as client:
                    return await client.start_flow(flow, flow_id, value)
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Accept a Flow ID and a JSON input path from the command line."""
    parser = argparse.ArgumentParser(description="Read a DocuSign envelope in Dex")
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    arguments = parser.parse_args()
    print(asyncio.run(start_envelope_status(arguments.flow_id, arguments.input_file)))


if __name__ == "__main__":
    main()
