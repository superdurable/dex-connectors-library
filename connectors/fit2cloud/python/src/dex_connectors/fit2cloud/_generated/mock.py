# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListPostsInput, ListPostsOutput, PostSummary
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_POSTS_DEFINITION, PROVIDER_REJECTED

def list_posts_listed(value: ListPostsOutput, failure: Failure | None = None) -> Case[ListPostsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_posts_listed, '__module__', "dex_connectors.fit2cloud.fit2cloudmock")

def list_posts_provider_rejected(value: ListPostsOutput, failure: Failure | None = None) -> Case[ListPostsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_posts_provider_rejected, '__module__', "dex_connectors.fit2cloud.fit2cloudmock")

def list_posts_invalid_response(value: ListPostsOutput, failure: Failure | None = None) -> Case[ListPostsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_posts_invalid_response, '__module__', "dex_connectors.fit2cloud.fit2cloudmock")

def list_posts_defect(value: ListPostsOutput, failure: Failure | None = None) -> Case[ListPostsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_posts_defect, '__module__', "dex_connectors.fit2cloud.fit2cloudmock")

def list_posts_retry(failure: Failure, after_seconds: float = 0) -> Case[ListPostsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_posts_retry, '__module__', "dex_connectors.fit2cloud.fit2cloudmock")

def list_posts_sample_posts() -> Case[ListPostsOutput]:
    return Case.branch_result(LISTED, ListPostsOutput(posts=(PostSummary(name='example-post', title='Example Post', slug='example-post', published=True, visibility='PUBLIC'),), next_page=0))

setattr(list_posts_sample_posts, '__module__', "dex_connectors.fit2cloud.fit2cloudmock")


class Mock:

    def __init__(self, connection_name: str = "fit2cloud") -> None:
        self.list_posts_script: Query[ListPostsInput, ListPostsOutput] = Query(LIST_POSTS_DEFINITION)
        self.list_posts_script.default(list_posts_sample_posts())
        self._connection = new_connection_with_operations(
            list_posts=self.list_posts_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.fit2cloud.fit2cloudmock")

__all__ = [
    "Mock",
    "list_posts_listed",
    "list_posts_provider_rejected",
    "list_posts_invalid_response",
    "list_posts_defect",
    "list_posts_retry",
    "list_posts_sample_posts",
]
