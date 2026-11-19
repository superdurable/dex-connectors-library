# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListTagsInput, ListTagsOutput, TagSummary
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_TAGS_DEFINITION, PROVIDER_REJECTED

def list_tags_listed(value: ListTagsOutput, failure: Failure | None = None) -> Case[ListTagsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_tags_listed, '__module__', "dex_connectors.jingdigital.jingdigitalmock")

def list_tags_provider_rejected(value: ListTagsOutput, failure: Failure | None = None) -> Case[ListTagsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_tags_provider_rejected, '__module__', "dex_connectors.jingdigital.jingdigitalmock")

def list_tags_invalid_response(value: ListTagsOutput, failure: Failure | None = None) -> Case[ListTagsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_tags_invalid_response, '__module__', "dex_connectors.jingdigital.jingdigitalmock")

def list_tags_defect(value: ListTagsOutput, failure: Failure | None = None) -> Case[ListTagsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_tags_defect, '__module__', "dex_connectors.jingdigital.jingdigitalmock")

def list_tags_retry(failure: Failure, after_seconds: float = 0) -> Case[ListTagsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_tags_retry, '__module__', "dex_connectors.jingdigital.jingdigitalmock")

def list_tags_sample_tags() -> Case[ListTagsOutput]:
    return Case.branch_result(LISTED, ListTagsOutput(tags=(TagSummary(id=166007, name='Example Tag', level=1),), total=1, next_page=0))

setattr(list_tags_sample_tags, '__module__', "dex_connectors.jingdigital.jingdigitalmock")


class Mock:

    def __init__(self, connection_name: str = "jingdigital") -> None:
        self.list_tags_script: Query[ListTagsInput, ListTagsOutput] = Query(LIST_TAGS_DEFINITION)
        self.list_tags_script.default(list_tags_sample_tags())
        self._connection = new_connection_with_operations(
            list_tags=self.list_tags_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.jingdigital.jingdigitalmock")

__all__ = [
    "Mock",
    "list_tags_listed",
    "list_tags_provider_rejected",
    "list_tags_invalid_response",
    "list_tags_defect",
    "list_tags_retry",
    "list_tags_sample_tags",
]
