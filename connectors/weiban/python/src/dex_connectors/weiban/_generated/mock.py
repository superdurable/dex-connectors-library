# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListTagsInput, TagMetadata, TagsResult
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_TAGS_DEFINITION, PROVIDER_REJECTED

def list_tags_listed(value: TagsResult, failure: Failure | None = None) -> Case[TagsResult]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_tags_listed, '__module__', "dex_connectors.weiban.weibanmock")

def list_tags_provider_rejected(value: TagsResult, failure: Failure | None = None) -> Case[TagsResult]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_tags_provider_rejected, '__module__', "dex_connectors.weiban.weibanmock")

def list_tags_invalid_response(value: TagsResult, failure: Failure | None = None) -> Case[TagsResult]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_tags_invalid_response, '__module__', "dex_connectors.weiban.weibanmock")

def list_tags_defect(value: TagsResult, failure: Failure | None = None) -> Case[TagsResult]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_tags_defect, '__module__', "dex_connectors.weiban.weibanmock")

def list_tags_retry(failure: Failure, after_seconds: float = 0) -> Case[TagsResult]:
    return Case.retry(failure, after_seconds)

setattr(list_tags_retry, '__module__', "dex_connectors.weiban.weibanmock")

def list_tags_listed_tags() -> Case[TagsResult]:
    return Case.branch_result(LISTED, TagsResult(total=1, tags=(TagMetadata(id='tag_123', name='Returning', group='Lifecycle'),)))

setattr(list_tags_listed_tags, '__module__', "dex_connectors.weiban.weibanmock")


class Mock:

    def __init__(self, connection_name: str = "weiban") -> None:
        self.list_tags_script: Query[ListTagsInput, TagsResult] = Query(LIST_TAGS_DEFINITION)
        self.list_tags_script.default(list_tags_listed_tags())
        self._connection = new_connection_with_operations(
            list_tags=self.list_tags_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.weiban.weibanmock")

__all__ = [
    "Mock",
    "list_tags_listed",
    "list_tags_provider_rejected",
    "list_tags_invalid_response",
    "list_tags_defect",
    "list_tags_retry",
    "list_tags_listed_tags",
]
