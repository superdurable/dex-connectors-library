# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListUserVideosInput, UserVideo, UserVideosPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_USER_VIDEOS_DEFINITION, PROVIDER_REJECTED

def list_user_videos_listed(value: UserVideosPage, failure: Failure | None = None) -> Case[UserVideosPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_user_videos_listed, '__module__', "dex_connectors.kuaishou.kuaishoumock")

def list_user_videos_provider_rejected(value: UserVideosPage, failure: Failure | None = None) -> Case[UserVideosPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_user_videos_provider_rejected, '__module__', "dex_connectors.kuaishou.kuaishoumock")

def list_user_videos_invalid_response(value: UserVideosPage, failure: Failure | None = None) -> Case[UserVideosPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_user_videos_invalid_response, '__module__', "dex_connectors.kuaishou.kuaishoumock")

def list_user_videos_defect(value: UserVideosPage, failure: Failure | None = None) -> Case[UserVideosPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_user_videos_defect, '__module__', "dex_connectors.kuaishou.kuaishoumock")

def list_user_videos_retry(failure: Failure, after_seconds: float = 0) -> Case[UserVideosPage]:
    return Case.retry(failure, after_seconds)

setattr(list_user_videos_retry, '__module__', "dex_connectors.kuaishou.kuaishoumock")

def list_user_videos_listed_default() -> Case[UserVideosPage]:
    return Case.branch_result(LISTED, UserVideosPage(videos=(UserVideo(photo_id='3xwn3kkerxj6g9n', caption='测试标题', created_at_milliseconds=1596629598574, like_count=1, comment_count=1, view_count=1, pending=False),), next_cursor='3xwn3kkerxj6g9n'))

setattr(list_user_videos_listed_default, '__module__', "dex_connectors.kuaishou.kuaishoumock")


class Mock:

    def __init__(self, connection_name: str = "kuaishou") -> None:
        self.list_user_videos_script: Query[ListUserVideosInput, UserVideosPage] = Query(LIST_USER_VIDEOS_DEFINITION)
        self.list_user_videos_script.default(list_user_videos_listed_default())
        self._connection = new_connection_with_operations(
            list_user_videos=self.list_user_videos_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.kuaishou.kuaishoumock")

__all__ = [
    "Mock",
    "list_user_videos_listed",
    "list_user_videos_provider_rejected",
    "list_user_videos_invalid_response",
    "list_user_videos_defect",
    "list_user_videos_retry",
    "list_user_videos_listed_default",
]
