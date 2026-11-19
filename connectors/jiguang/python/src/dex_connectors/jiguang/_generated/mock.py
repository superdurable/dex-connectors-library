# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ApplicationTags, ListApplicationTagsInput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_APPLICATION_TAGS_DEFINITION, PROVIDER_REJECTED

def list_application_tags_listed(value: ApplicationTags, failure: Failure | None = None) -> Case[ApplicationTags]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_application_tags_listed, '__module__', "dex_connectors.jiguang.jiguangmock")

def list_application_tags_provider_rejected(value: ApplicationTags, failure: Failure | None = None) -> Case[ApplicationTags]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_application_tags_provider_rejected, '__module__', "dex_connectors.jiguang.jiguangmock")

def list_application_tags_invalid_response(value: ApplicationTags, failure: Failure | None = None) -> Case[ApplicationTags]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_application_tags_invalid_response, '__module__', "dex_connectors.jiguang.jiguangmock")

def list_application_tags_defect(value: ApplicationTags, failure: Failure | None = None) -> Case[ApplicationTags]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_application_tags_defect, '__module__', "dex_connectors.jiguang.jiguangmock")

def list_application_tags_retry(failure: Failure, after_seconds: float = 0) -> Case[ApplicationTags]:
    return Case.retry(failure, after_seconds)

setattr(list_application_tags_retry, '__module__', "dex_connectors.jiguang.jiguangmock")

def list_application_tags_listed_default() -> Case[ApplicationTags]:
    return Case.branch_result(LISTED, ApplicationTags(tags=('news', 'alerts')))

setattr(list_application_tags_listed_default, '__module__', "dex_connectors.jiguang.jiguangmock")


class Mock:

    def __init__(self, connection_name: str = "jiguang") -> None:
        self.list_application_tags_script: Query[ListApplicationTagsInput, ApplicationTags] = Query(LIST_APPLICATION_TAGS_DEFINITION)
        self.list_application_tags_script.default(list_application_tags_listed_default())
        self._connection = new_connection_with_operations(
            list_application_tags=self.list_application_tags_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.jiguang.jiguangmock")

__all__ = [
    "Mock",
    "list_application_tags_listed",
    "list_application_tags_provider_rejected",
    "list_application_tags_invalid_response",
    "list_application_tags_defect",
    "list_application_tags_retry",
    "list_application_tags_listed_default",
]
