# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListTablesInput, TablesPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_TABLES_DEFINITION, PROVIDER_REJECTED

def list_tables_listed(value: TablesPage, failure: Failure | None = None) -> Case[TablesPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_tables_listed, '__module__', "dex_connectors.clickhouse.clickhousemock")

def list_tables_provider_rejected(value: TablesPage, failure: Failure | None = None) -> Case[TablesPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_tables_provider_rejected, '__module__', "dex_connectors.clickhouse.clickhousemock")

def list_tables_invalid_response(value: TablesPage, failure: Failure | None = None) -> Case[TablesPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_tables_invalid_response, '__module__', "dex_connectors.clickhouse.clickhousemock")

def list_tables_defect(value: TablesPage, failure: Failure | None = None) -> Case[TablesPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_tables_defect, '__module__', "dex_connectors.clickhouse.clickhousemock")

def list_tables_retry(failure: Failure, after_seconds: float = 0) -> Case[TablesPage]:
    return Case.retry(failure, after_seconds)

setattr(list_tables_retry, '__module__', "dex_connectors.clickhouse.clickhousemock")

def list_tables_listed_default() -> Case[TablesPage]:
    return Case.branch_result(LISTED, TablesPage(database='analytics', names=('events', 'users'), has_more=False))

setattr(list_tables_listed_default, '__module__', "dex_connectors.clickhouse.clickhousemock")


class Mock:

    def __init__(self, connection_name: str = "clickhouse") -> None:
        self.list_tables_script: Query[ListTablesInput, TablesPage] = Query(LIST_TABLES_DEFINITION)
        self.list_tables_script.default(list_tables_listed_default())
        self._connection = new_connection_with_operations(
            list_tables=self.list_tables_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.clickhouse.clickhousemock")

__all__ = [
    "Mock",
    "list_tables_listed",
    "list_tables_provider_rejected",
    "list_tables_invalid_response",
    "list_tables_defect",
    "list_tables_retry",
    "list_tables_listed_default",
]
