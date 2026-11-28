# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the profile Flow through Dex using a public username or a retrying fixture."""

import asyncio
import os
import sys
from datetime import timedelta
from tempfile import TemporaryDirectory
from uuid import uuid4

import dex
import httpx
from dex_connectors.sdk import ConnectionRef

from dex_connectors.hackernews import Client, Config, User, new_connection

from .profile_flow import CONNECTION_NAME, ProfileFlow, ProfileRequest


async def read_profile(provider: Client, username: str) -> User:
    """Start a real Worker and wait for the requested profile."""
    connection = new_connection(provider, ConnectionRef("hacker-news", CONNECTION_NAME))
    flow = ProfileFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
    try:
        with TemporaryDirectory() as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 1 << 24))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
            serving = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=15)
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=address)
                ) as client:
                    flow_id = f"hackernews-profile-python-{uuid4().hex}"
                    await client.start_flow(flow, flow_id, ProfileRequest(username))
                    result = await client.wait_for_flow(flow_id, timedelta(seconds=30))
                    if result.status != dex.FlowStatus.COMPLETED:
                        raise RuntimeError(f"profile Flow failed: {result.error_message}")
                    return result.single_output(User)
            finally:
                await worker.stop()
                await serving
                cache.close()
    finally:
        await connection.aclose()


async def verify_retry() -> None:
    """Verify the Worker retries one 503 and returns the canonical public profile."""
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        assert request.url.raw_path == b"/user/a%20b+me.json"
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(200, json={"id": "a b+me", "karma": 12, "profileUrl": "spoofed"})

    provider = Client(
        Config(base_url="http://127.0.0.1:12345"), httpx.AsyncClient(transport=httpx.MockTransport(respond))
    )
    result = await read_profile(provider, "a b+me")
    assert result == User(id="a b+me", karma=12, profile_url="https://news.ycombinator.com/user?id=a+b%2Bme")
    assert len(requests) == 2


if __name__ == "__main__":
    if len(sys.argv) == 1:
        asyncio.run(verify_retry())
    elif len(sys.argv) == 2:
        print(asyncio.run(read_profile(Client(Config()), sys.argv[1])).profile_url)
    else:
        raise SystemExit("usage: python -m examples.top_story.profile_integration [public-username]")
