# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start a Notion submission Flow from a bounded JSON file."""

import asyncio
import os
import sys
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.notion import new_project_connection

from .flow import CONNECTION_NAME, SubmissionFlow, SubmissionRequest


async def start_submission(flow_id: str, input_path: Path) -> str:
    """Decode one request and submit it to a registered Dex Flow."""
    encoded = input_path.read_bytes()[:65_537]
    if len(encoded) > 65_536:
        raise ValueError("submission input exceeds 65536 bytes")
    request = (
        dex.CodecRegistry()
        .resolve(SubmissionRequest)
        .decode(dex.Value(dex.WireKind.JSON, encoded.decode("utf-8")))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = SubmissionFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="notion-python-start-blobs-") as directory:
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
    """Accept a unique Flow ID and the JSON request path."""
    if len(sys.argv) != 3:
        raise SystemExit("usage: python -m examples.submission_intake.start <flow-id> <input.json>")
    print(asyncio.run(start_submission(sys.argv[1], Path(sys.argv[2]))))


if __name__ == "__main__":
    main()
