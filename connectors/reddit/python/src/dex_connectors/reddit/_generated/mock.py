# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetPostInput, Post
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_POST_DEFINITION, INVALID_RESPONSE, MISSING, PROVIDER_REJECTED

def get_post_found(value: Post, failure: Failure | None = None) -> Case[Post]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_post_found, '__module__', "dex_connectors.reddit.redditmock")

def get_post_missing(value: Post, failure: Failure | None = None) -> Case[Post]:
    return Case.branch_result(MISSING, value, failure)

setattr(get_post_missing, '__module__', "dex_connectors.reddit.redditmock")

def get_post_provider_rejected(value: Post, failure: Failure | None = None) -> Case[Post]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_post_provider_rejected, '__module__', "dex_connectors.reddit.redditmock")

def get_post_invalid_response(value: Post, failure: Failure | None = None) -> Case[Post]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_post_invalid_response, '__module__', "dex_connectors.reddit.redditmock")

def get_post_defect(value: Post, failure: Failure | None = None) -> Case[Post]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_post_defect, '__module__', "dex_connectors.reddit.redditmock")

def get_post_retry(failure: Failure, after_seconds: float = 0) -> Case[Post]:
    return Case.retry(failure, after_seconds)

setattr(get_post_retry, '__module__', "dex_connectors.reddit.redditmock")

def get_post_found_default() -> Case[Post]:
    return Case.branch_result(FOUND, Post(id='t3_abc123', title='Example post', subreddit='example', permalink='/r/example/comments/abc123/example_post/', score=42))

setattr(get_post_found_default, '__module__', "dex_connectors.reddit.redditmock")


class Mock:

    def __init__(self, connection_name: str = "reddit") -> None:
        self.get_post_script: Query[GetPostInput, Post] = Query(GET_POST_DEFINITION)
        self.get_post_script.default(get_post_found_default())
        self._connection = new_connection_with_operations(
            get_post=self.get_post_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.reddit.redditmock")

__all__ = [
    "Mock",
    "get_post_found",
    "get_post_missing",
    "get_post_provider_rejected",
    "get_post_invalid_response",
    "get_post_defect",
    "get_post_retry",
    "get_post_found_default",
]
