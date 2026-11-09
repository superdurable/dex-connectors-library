# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Reddit getPost decisions matching the Go-recorded provider scenarios."""

from __future__ import annotations

import json
import re
from typing import Any
from urllib.parse import urlencode

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import GetPostInput, Post

_POST_ID = re.compile(r"^t3_[a-z0-9]{1,20}$")


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "reddit", "getPost", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[Post]:
    return QueryAttempt.branch_result(branch, Post(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[Post]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _read_failure(exchange: Exchange) -> QueryAttempt[Post] | None:
    if exchange.error == "credential":
        return _branch(
            "defect", FailureKind.AUTHENTICATION, "Reddit OAuth credentials are unavailable or invalid"
        )
    if exchange.error in ("request", "refresh"):
        return _retry(FailureKind.TRANSPORT, "Reddit request failed")
    if exchange.status == 429:
        return _retry(FailureKind.RATE_LIMIT, "Reddit rate limited the request", exchange.retry_after)
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(FailureKind.AVAILABILITY, "Reddit is temporarily unavailable", exchange.retry_after)
    if exchange.error == "too_large":
        return _branch(
            "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Reddit response exceeded the configured limit"
        )
    if exchange.error:
        return _retry(FailureKind.TRANSPORT, "Reddit response was interrupted")
    if exchange.status != 200:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, f"Reddit rejected HTTP {exchange.status}"
        )
    return None


def _decode_post(body: bytes, post_id: str) -> tuple[Post, bool] | None:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    if not isinstance(document, dict) or document.get("kind") != "Listing":
        return None
    data = document.get("data")
    children = data.get("children") if isinstance(data, dict) else None
    if not isinstance(children, list) or len(children) > 1:
        return None
    if not children:
        return Post(), False
    child = children[0]
    if not isinstance(child, dict) or child.get("kind") != "t3":
        return None
    post = child.get("data")
    if not isinstance(post, dict) or post.get("name") != post_id:
        return None
    title, subreddit, permalink, score = (
        post.get(key) for key in ("title", "subreddit", "permalink", "score")
    )
    if (
        not isinstance(title, str)
        or not title
        or not isinstance(subreddit, str)
        or not subreddit
        or not isinstance(permalink, str)
        or not permalink.startswith("/r/")
        or any(char in permalink for char in "\x00\r\n")
        or not isinstance(score, int)
        or isinstance(score, bool)
        or not -(2**63) <= score < 2**63
    ):
        return None
    return Post(post_id, title, subreddit, permalink, score), True


class GetPostOperation:
    """Read one post with an approved Reddit OAuth application."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated post Query contract."""
        return definitions.GET_POST_DEFINITION

    async def invoke(self, call: Call, input: GetPostInput) -> QueryAttempt[Post]:
        """Read one bounded listing and return only selected public metadata."""
        if not _POST_ID.fullmatch(input.post_id):
            return _branch("defect", FailureKind.VALIDATION, "Reddit post ID must be a full t3_ identifier")
        exchange = await self.client.get(
            call, "/api/info?" + urlencode({"id": input.post_id, "raw_json": "1"})
        )
        failure = _read_failure(exchange)
        if failure is not None:
            return failure
        decoded = _decode_post(exchange.body, input.post_id)
        if decoded is None:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Reddit returned invalid post JSON")
        post, found = decoded
        return QueryAttempt.branch_result("found" if found else "missing", post, Receipt())
