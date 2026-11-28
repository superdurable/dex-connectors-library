# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the two checked-in search Flows against fixtures or the public index."""

import asyncio
import sys

import httpx

from dex_connectors.hackernews import Client, Config, ItemSearchPage, StorySearchPage

from .item_search_flow import CONNECTION_NAME, ItemSearchFlow, ItemSearchRequest
from .query_integration import read_once
from .story_search_flow import StorySearchFlow, StorySearchRequest


class SearchResponses:
    def __init__(self, kind: str, query: str) -> None:
        self.kind = kind
        self.query = query
        self.calls: list[httpx.Request] = []

    def respond(self, request: httpx.Request) -> httpx.Response:
        self.calls.append(request)
        assert request.url.host == "hn.algolia.com"
        assert request.url.path == "/api/v1/search"
        assert request.url.params.get("tags") == ("story" if self.kind == "story" else None)
        assert request.url.params["query"] == self.query
        if len(self.calls) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "page": 0,
                "nbPages": 2,
                "nbHits": 3,
                "hits": [{"objectID": "42", "created_at_i": 1, "_tags": ["story"], "title": "Release"}],
            },
        )


async def run_searches(query: str, *, live: bool) -> None:
    for kind in ("story", "item"):
        responses = SearchResponses(kind, query)
        provider = (
            Client(Config())
            if live
            else Client(Config(), httpx.AsyncClient(transport=httpx.MockTransport(responses.respond)))
        )
        if kind == "story":
            stories = await read_once(
                provider, CONNECTION_NAME, StorySearchFlow, StorySearchRequest(query), StorySearchPage
            )
            ids = [hit.id for hit in stories.stories]
            next_page = stories.next_page
        else:
            items = await read_once(
                provider, CONNECTION_NAME, ItemSearchFlow, ItemSearchRequest(query), ItemSearchPage
            )
            ids = [hit.id for hit in items.items]
            next_page = items.next_page
        if live:
            print(kind, ids, next_page)
        else:
            assert (ids, next_page, len(responses.calls)) == ([42], 1, 2)


if __name__ == "__main__":
    if len(sys.argv) > 2:
        raise SystemExit("usage: python -m examples.top_story.search_integration [keywords]")
    asyncio.run(run_searches(sys.argv[1] if len(sys.argv) == 2 else "compiler", live=len(sys.argv) == 2))
