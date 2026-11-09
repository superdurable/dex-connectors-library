# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CreatePostInput, CreatedPost, GetPostInput, ListPostsInput, Post, PostPage, RenderedText
from .definitions import CONNECTOR_ID, CREATED, CREATE_POST_DEFINITION, DEFECT, FOUND, GET_POST_DEFINITION, INVALID_RESPONSE, LISTED, LIST_POSTS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, UNCERTAIN

def create_post_created(value: CreatedPost, failure: Failure | None = None) -> Case[CreatedPost]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_post_created, '__module__', "dex_connectors.wordpress.wordpressmock")

def create_post_provider_rejected(value: CreatedPost, failure: Failure | None = None) -> Case[CreatedPost]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_post_provider_rejected, '__module__', "dex_connectors.wordpress.wordpressmock")

def create_post_uncertain(value: CreatedPost, failure: Failure) -> Case[CreatedPost]:
    return Case.uncertain(value, failure)

setattr(create_post_uncertain, '__module__', "dex_connectors.wordpress.wordpressmock")

def create_post_defect(value: CreatedPost, failure: Failure | None = None) -> Case[CreatedPost]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_post_defect, '__module__', "dex_connectors.wordpress.wordpressmock")

def create_post_retry(failure: Failure, after_seconds: float = 0) -> Case[CreatedPost]:
    return Case.retry(failure, after_seconds)

setattr(create_post_retry, '__module__', "dex_connectors.wordpress.wordpressmock")

def create_post_created_draft() -> Case[CreatedPost]:
    return Case.branch_result(CREATED, CreatedPost(id=43, status='draft'))

setattr(create_post_created_draft, '__module__', "dex_connectors.wordpress.wordpressmock")

def get_post_found(value: Post, failure: Failure | None = None) -> Case[Post]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_post_found, '__module__', "dex_connectors.wordpress.wordpressmock")

def get_post_not_found(value: Post, failure: Failure | None = None) -> Case[Post]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_post_not_found, '__module__', "dex_connectors.wordpress.wordpressmock")

def get_post_provider_rejected(value: Post, failure: Failure | None = None) -> Case[Post]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_post_provider_rejected, '__module__', "dex_connectors.wordpress.wordpressmock")

def get_post_invalid_response(value: Post, failure: Failure | None = None) -> Case[Post]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_post_invalid_response, '__module__', "dex_connectors.wordpress.wordpressmock")

def get_post_defect(value: Post, failure: Failure | None = None) -> Case[Post]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_post_defect, '__module__', "dex_connectors.wordpress.wordpressmock")

def get_post_retry(failure: Failure, after_seconds: float = 0) -> Case[Post]:
    return Case.retry(failure, after_seconds)

setattr(get_post_retry, '__module__', "dex_connectors.wordpress.wordpressmock")

def get_post_published_post() -> Case[Post]:
    return Case.branch_result(FOUND, Post(id=42, slug='meridian-release', status='publish', link='https://example.test/meridian-release/', title=RenderedText(rendered='Meridian release'), content=RenderedText(rendered='<p>Released.</p>'), date_gmt='2026-11-07T12:00:00'))

setattr(get_post_published_post, '__module__', "dex_connectors.wordpress.wordpressmock")

def list_posts_listed(value: PostPage, failure: Failure | None = None) -> Case[PostPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_posts_listed, '__module__', "dex_connectors.wordpress.wordpressmock")

def list_posts_provider_rejected(value: PostPage, failure: Failure | None = None) -> Case[PostPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_posts_provider_rejected, '__module__', "dex_connectors.wordpress.wordpressmock")

def list_posts_invalid_response(value: PostPage, failure: Failure | None = None) -> Case[PostPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_posts_invalid_response, '__module__', "dex_connectors.wordpress.wordpressmock")

def list_posts_defect(value: PostPage, failure: Failure | None = None) -> Case[PostPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_posts_defect, '__module__', "dex_connectors.wordpress.wordpressmock")

def list_posts_retry(failure: Failure, after_seconds: float = 0) -> Case[PostPage]:
    return Case.retry(failure, after_seconds)

setattr(list_posts_retry, '__module__', "dex_connectors.wordpress.wordpressmock")

def list_posts_published_posts() -> Case[PostPage]:
    return Case.branch_result(LISTED, PostPage(posts=(Post(id=42, slug='meridian-release', status='publish', link='https://example.test/meridian-release/', title=RenderedText(rendered='Meridian release'), content=RenderedText(rendered='<p>Released.</p>'), date_gmt='2026-11-07T12:00:00'),), page=1, total=1, total_pages=1))

setattr(list_posts_published_posts, '__module__', "dex_connectors.wordpress.wordpressmock")


class Mock:

    def __init__(self, connection_name: str = "wordpress") -> None:
        self.create_post_script: Mutation[CreatePostInput, CreatedPost] = Mutation(CREATE_POST_DEFINITION)
        self.create_post_script.default(create_post_created_draft())
        self.get_post_script: Query[GetPostInput, Post] = Query(GET_POST_DEFINITION)
        self.get_post_script.default(get_post_published_post())
        self.list_posts_script: Query[ListPostsInput, PostPage] = Query(LIST_POSTS_DEFINITION)
        self.list_posts_script.default(list_posts_published_posts())
        self._connection = new_connection_with_operations(
            create_post=self.create_post_script,
            get_post=self.get_post_script,
            list_posts=self.list_posts_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.wordpress.wordpressmock")

__all__ = [
    "Mock",
    "create_post_created",
    "create_post_provider_rejected",
    "create_post_uncertain",
    "create_post_defect",
    "create_post_retry",
    "create_post_created_draft",
    "get_post_found",
    "get_post_not_found",
    "get_post_provider_rejected",
    "get_post_invalid_response",
    "get_post_defect",
    "get_post_retry",
    "get_post_published_post",
    "list_posts_listed",
    "list_posts_provider_rejected",
    "list_posts_invalid_response",
    "list_posts_defect",
    "list_posts_retry",
    "list_posts_published_posts",
]
