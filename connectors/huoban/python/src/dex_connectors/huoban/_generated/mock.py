# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListSpacesInput, ListSpacesOutput, SpaceMetadata
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_SPACES_DEFINITION, PROVIDER_REJECTED

def list_spaces_listed(value: ListSpacesOutput, failure: Failure | None = None) -> Case[ListSpacesOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_spaces_listed, '__module__', "dex_connectors.huoban.huobanmock")

def list_spaces_provider_rejected(value: ListSpacesOutput, failure: Failure | None = None) -> Case[ListSpacesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_spaces_provider_rejected, '__module__', "dex_connectors.huoban.huobanmock")

def list_spaces_invalid_response(value: ListSpacesOutput, failure: Failure | None = None) -> Case[ListSpacesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_spaces_invalid_response, '__module__', "dex_connectors.huoban.huobanmock")

def list_spaces_defect(value: ListSpacesOutput, failure: Failure | None = None) -> Case[ListSpacesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_spaces_defect, '__module__', "dex_connectors.huoban.huobanmock")

def list_spaces_retry(failure: Failure, after_seconds: float = 0) -> Case[ListSpacesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_spaces_retry, '__module__', "dex_connectors.huoban.huobanmock")

def list_spaces_one_space() -> Case[ListSpacesOutput]:
    return Case.branch_result(LISTED, ListSpacesOutput(spaces=(SpaceMetadata(space_id='4000000000000001', name='Operations'),)))

setattr(list_spaces_one_space, '__module__', "dex_connectors.huoban.huobanmock")


class Mock:

    def __init__(self, connection_name: str = "huoban") -> None:
        self.list_spaces_script: Query[ListSpacesInput, ListSpacesOutput] = Query(LIST_SPACES_DEFINITION)
        self.list_spaces_script.default(list_spaces_one_space())
        self._connection = new_connection_with_operations(
            list_spaces=self.list_spaces_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.huoban.huobanmock")

__all__ = [
    "Mock",
    "list_spaces_listed",
    "list_spaces_provider_rejected",
    "list_spaces_invalid_response",
    "list_spaces_defect",
    "list_spaces_retry",
    "list_spaces_one_space",
]
