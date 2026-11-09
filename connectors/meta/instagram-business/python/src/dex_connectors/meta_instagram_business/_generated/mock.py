# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import InstagramMedia, ListMediaInput, MediaListPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_MEDIA_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_media_listed(value: MediaListPage, failure: Failure | None = None) -> Case[MediaListPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_media_listed, '__module__', "dex_connectors.meta_instagram_business.instagrambusinessmock")

def list_media_not_found(value: MediaListPage, failure: Failure | None = None) -> Case[MediaListPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_media_not_found, '__module__', "dex_connectors.meta_instagram_business.instagrambusinessmock")

def list_media_provider_rejected(value: MediaListPage, failure: Failure | None = None) -> Case[MediaListPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_media_provider_rejected, '__module__', "dex_connectors.meta_instagram_business.instagrambusinessmock")

def list_media_invalid_response(value: MediaListPage, failure: Failure | None = None) -> Case[MediaListPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_media_invalid_response, '__module__', "dex_connectors.meta_instagram_business.instagrambusinessmock")

def list_media_defect(value: MediaListPage, failure: Failure | None = None) -> Case[MediaListPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_media_defect, '__module__', "dex_connectors.meta_instagram_business.instagrambusinessmock")

def list_media_retry(failure: Failure, after_seconds: float = 0) -> Case[MediaListPage]:
    return Case.retry(failure, after_seconds)

setattr(list_media_retry, '__module__', "dex_connectors.meta_instagram_business.instagrambusinessmock")

def list_media_listed_default() -> Case[MediaListPage]:
    return Case.branch_result(LISTED, MediaListPage(media=(InstagramMedia(id='17900000000000001', caption='Hello', timestamp='2026-11-08T10:00:00+0000', permalink='https://www.instagram.com/p/example/', media_type='IMAGE'),), next_cursor=''))

setattr(list_media_listed_default, '__module__', "dex_connectors.meta_instagram_business.instagrambusinessmock")


class Mock:

    def __init__(self, connection_name: str = "instagram-business") -> None:
        self.list_media_script: Query[ListMediaInput, MediaListPage] = Query(LIST_MEDIA_DEFINITION)
        self.list_media_script.default(list_media_listed_default())
        self._connection = new_connection_with_operations(
            list_media=self.list_media_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.meta_instagram_business.instagrambusinessmock")

__all__ = [
    "Mock",
    "list_media_listed",
    "list_media_not_found",
    "list_media_provider_rejected",
    "list_media_invalid_response",
    "list_media_defect",
    "list_media_retry",
    "list_media_listed_default",
]
