# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read an article by public ID, or verify Worker retries using a fixed response."""

import asyncio
import sys

import httpx

from dex_connectors.hackernews import ArticleThread, Client, Config

from .article_flow import CONNECTION_NAME, ArticleRequest, ArticleThreadFlow
from .query_integration import read_once


async def read_article(provider: Client, article_id: int) -> ArticleThread:
    """Run the checked-in Flow for one article."""
    return await read_once(
        provider, CONNECTION_NAME, ArticleThreadFlow, ArticleRequest(article_id), ArticleThread
    )


async def verify_retry() -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        assert str(request.url) == "https://hn.algolia.com/api/v1/items/42"
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200, json={"id": 42, "title": "Article", "children": [{"id": 7, "children": [{"id": 9}]}]}
        )

    provider = Client(Config(), httpx.AsyncClient(transport=httpx.MockTransport(respond)))
    article = await read_article(provider, 42)
    assert article.comments[0].replies[0].discussion_url == "https://news.ycombinator.com/item?id=9"
    assert len(requests) == 2


if __name__ == "__main__":
    if len(sys.argv) == 1:
        asyncio.run(verify_retry())
    elif len(sys.argv) == 2:
        article = asyncio.run(read_article(Client(Config()), int(sys.argv[1])))
        print(article.discussion_url, len(article.comments))
    else:
        raise SystemExit("usage: python -m examples.top_story.article_integration [article-id]")
