# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListProvincesInput, ListProvincesOutput, Province
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_PROVINCES_DEFINITION, PROVIDER_REJECTED

def list_provinces_listed(value: ListProvincesOutput, failure: Failure | None = None) -> Case[ListProvincesOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_provinces_listed, '__module__', "dex_connectors.jisuapi.jisuapimock")

def list_provinces_provider_rejected(value: ListProvincesOutput, failure: Failure | None = None) -> Case[ListProvincesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_provinces_provider_rejected, '__module__', "dex_connectors.jisuapi.jisuapimock")

def list_provinces_invalid_response(value: ListProvincesOutput, failure: Failure | None = None) -> Case[ListProvincesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_provinces_invalid_response, '__module__', "dex_connectors.jisuapi.jisuapimock")

def list_provinces_defect(value: ListProvincesOutput, failure: Failure | None = None) -> Case[ListProvincesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_provinces_defect, '__module__', "dex_connectors.jisuapi.jisuapimock")

def list_provinces_retry(failure: Failure, after_seconds: float = 0) -> Case[ListProvincesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_provinces_retry, '__module__', "dex_connectors.jisuapi.jisuapimock")

def list_provinces_one_province() -> Case[ListProvincesOutput]:
    return Case.branch_result(LISTED, ListProvincesOutput(provinces=(Province(id='1', name='Beijing'),)))

setattr(list_provinces_one_province, '__module__', "dex_connectors.jisuapi.jisuapimock")


class Mock:

    def __init__(self, connection_name: str = "jisuapi") -> None:
        self.list_provinces_script: Query[ListProvincesInput, ListProvincesOutput] = Query(LIST_PROVINCES_DEFINITION)
        self.list_provinces_script.default(list_provinces_one_province())
        self._connection = new_connection_with_operations(
            list_provinces=self.list_provinces_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.jisuapi.jisuapimock")

__all__ = [
    "Mock",
    "list_provinces_listed",
    "list_provinces_provider_rejected",
    "list_provinces_invalid_response",
    "list_provinces_defect",
    "list_provinces_retry",
    "list_provinces_one_province",
]
