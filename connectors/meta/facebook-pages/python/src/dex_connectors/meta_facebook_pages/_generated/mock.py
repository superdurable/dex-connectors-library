# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetPostInput, ListPostsInput, PagePostMetadata, PagePostsPage, PublishTextPostInput, PublishedPost
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_POST_DEFINITION, INVALID_RESPONSE, LISTED, LIST_POSTS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, PUBLISHED, PUBLISH_TEXT_POST_DEFINITION, UNCERTAIN

def list_posts_listed(value: PagePostsPage, failure: Failure | None = None) -> Case[PagePostsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_posts_listed, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def list_posts_not_found(value: PagePostsPage, failure: Failure | None = None) -> Case[PagePostsPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_posts_not_found, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def list_posts_provider_rejected(value: PagePostsPage, failure: Failure | None = None) -> Case[PagePostsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_posts_provider_rejected, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def list_posts_invalid_response(value: PagePostsPage, failure: Failure | None = None) -> Case[PagePostsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_posts_invalid_response, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def list_posts_defect(value: PagePostsPage, failure: Failure | None = None) -> Case[PagePostsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_posts_defect, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def list_posts_retry(failure: Failure, after_seconds: float = 0) -> Case[PagePostsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_posts_retry, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def list_posts_listed_default() -> Case[PagePostsPage]:
    return Case.branch_result(LISTED, PagePostsPage(posts=(PagePostMetadata(id='123456789_987654321', message='Welcome to our Page', created_time='2026-11-08T10:00:00+0000', permalink_url='https://www.facebook.com/123456789/posts/987654321'),), next_cursor=''))

setattr(list_posts_listed_default, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def get_post_found(value: PagePostMetadata, failure: Failure | None = None) -> Case[PagePostMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_post_found, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def get_post_not_found(value: PagePostMetadata, failure: Failure | None = None) -> Case[PagePostMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_post_not_found, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def get_post_provider_rejected(value: PagePostMetadata, failure: Failure | None = None) -> Case[PagePostMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_post_provider_rejected, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def get_post_invalid_response(value: PagePostMetadata, failure: Failure | None = None) -> Case[PagePostMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_post_invalid_response, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def get_post_defect(value: PagePostMetadata, failure: Failure | None = None) -> Case[PagePostMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_post_defect, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def get_post_retry(failure: Failure, after_seconds: float = 0) -> Case[PagePostMetadata]:
    return Case.retry(failure, after_seconds)

setattr(get_post_retry, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def get_post_found_default() -> Case[PagePostMetadata]:
    return Case.branch_result(FOUND, PagePostMetadata(id='123456789_987654321', message='Welcome to our Page', created_time='2026-11-08T10:00:00+0000', permalink_url='https://www.facebook.com/123456789/posts/987654321'))

setattr(get_post_found_default, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def publish_text_post_published(value: PublishedPost, failure: Failure | None = None) -> Case[PublishedPost]:
    return Case.branch_result(PUBLISHED, value, failure)

setattr(publish_text_post_published, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def publish_text_post_provider_rejected(value: PublishedPost, failure: Failure | None = None) -> Case[PublishedPost]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(publish_text_post_provider_rejected, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def publish_text_post_uncertain(value: PublishedPost, failure: Failure) -> Case[PublishedPost]:
    return Case.uncertain(value, failure)

setattr(publish_text_post_uncertain, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def publish_text_post_defect(value: PublishedPost, failure: Failure | None = None) -> Case[PublishedPost]:
    return Case.branch_result(DEFECT, value, failure)

setattr(publish_text_post_defect, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def publish_text_post_retry(failure: Failure, after_seconds: float = 0) -> Case[PublishedPost]:
    return Case.retry(failure, after_seconds)

setattr(publish_text_post_retry, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

def publish_text_post_published_default() -> Case[PublishedPost]:
    return Case.branch_result(PUBLISHED, PublishedPost(post_id='123456789_987654321'))

setattr(publish_text_post_published_default, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")


class Mock:

    def __init__(self, connection_name: str = "facebook-pages") -> None:
        self.list_posts_script: Query[ListPostsInput, PagePostsPage] = Query(LIST_POSTS_DEFINITION)
        self.list_posts_script.default(list_posts_listed_default())
        self.get_post_script: Query[GetPostInput, PagePostMetadata] = Query(GET_POST_DEFINITION)
        self.get_post_script.default(get_post_found_default())
        self.publish_text_post_script: Mutation[PublishTextPostInput, PublishedPost] = Mutation(PUBLISH_TEXT_POST_DEFINITION)
        self.publish_text_post_script.default(publish_text_post_published_default())
        self._connection = new_connection_with_operations(
            list_posts=self.list_posts_script,
            get_post=self.get_post_script,
            publish_text_post=self.publish_text_post_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.meta_facebook_pages.facebookpagesmock")

__all__ = [
    "Mock",
    "list_posts_listed",
    "list_posts_not_found",
    "list_posts_provider_rejected",
    "list_posts_invalid_response",
    "list_posts_defect",
    "list_posts_retry",
    "list_posts_listed_default",
    "get_post_found",
    "get_post_not_found",
    "get_post_provider_rejected",
    "get_post_invalid_response",
    "get_post_defect",
    "get_post_retry",
    "get_post_found_default",
    "publish_text_post_published",
    "publish_text_post_provider_rejected",
    "publish_text_post_uncertain",
    "publish_text_post_defect",
    "publish_text_post_retry",
    "publish_text_post_published_default",
]
