# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the profile Flow through Dex using a public username or a retrying fixture."""

import asyncio
import sys

import httpx

from dex_connectors.hackernews import Client, Config, User

from .profile_flow import CONNECTION_NAME, ProfileFlow, ProfileRequest
from .query_integration import read_once


async def read_profile(provider: Client, username: str) -> User:
    """Start a real Worker and wait for the requested profile."""
    return await read_once(provider, CONNECTION_NAME, ProfileFlow, ProfileRequest(username), User)


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
