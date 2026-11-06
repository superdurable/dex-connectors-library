# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start the Jira issue lookup Flow from a JSON input file."""

import argparse
import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.atlassian_jira import GetIssueInput, new_project_connection

from .flow import CONNECTION_NAME, IssueLookupFlow


async def start_issue_lookup(flow_id: str, input_file: Path) -> str:
    """Decode one bounded input and submit it to Dex."""
    contents = input_file.read_bytes()
    if len(contents) > 65536:
        raise ValueError("Jira issue lookup input exceeds 65536 bytes")
    input_value = (
        dex.CodecRegistry()
        .resolve(GetIssueInput)
        .decode(dex.Value(dex.WireKind.JSON, contents.decode("utf-8")))
    )
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        flow = IssueLookupFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="jira-python-start-blobs-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
            try:
                options = dex.ClientOptions(
                    server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                )
                async with dex.AsyncClient(registry, cache, options) as client:
                    return await client.start_flow(flow, flow_id, input_value)
            finally:
                cache.close()
    finally:
        await connection.aclose()
        await project.close()


def main() -> None:
    """Read the Flow ID and JSON path from CLI arguments."""
    parser = argparse.ArgumentParser(description="Start a Jira issue lookup Flow")
    parser.add_argument("flow_id")
    parser.add_argument("input_file", type=Path)
    args = parser.parse_args()
    print(asyncio.run(start_issue_lookup(args.flow_id, args.input_file)))


if __name__ == "__main__":
    main()
