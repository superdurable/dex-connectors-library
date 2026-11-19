# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import DatabaseMetadata, ListDatabasesInput, ListDatabasesOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_DATABASES_DEFINITION, PROVIDER_REJECTED

def list_databases_listed(value: ListDatabasesOutput, failure: Failure | None = None) -> Case[ListDatabasesOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_databases_listed, '__module__', "dex_connectors.taosdata.taosdatamock")

def list_databases_provider_rejected(value: ListDatabasesOutput, failure: Failure | None = None) -> Case[ListDatabasesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_databases_provider_rejected, '__module__', "dex_connectors.taosdata.taosdatamock")

def list_databases_invalid_response(value: ListDatabasesOutput, failure: Failure | None = None) -> Case[ListDatabasesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_databases_invalid_response, '__module__', "dex_connectors.taosdata.taosdatamock")

def list_databases_defect(value: ListDatabasesOutput, failure: Failure | None = None) -> Case[ListDatabasesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_databases_defect, '__module__', "dex_connectors.taosdata.taosdatamock")

def list_databases_retry(failure: Failure, after_seconds: float = 0) -> Case[ListDatabasesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_databases_retry, '__module__', "dex_connectors.taosdata.taosdatamock")

def list_databases_sample() -> Case[ListDatabasesOutput]:
    return Case.branch_result(LISTED, ListDatabasesOutput(databases=(DatabaseMetadata(name='information_schema', table_count=16, status='ready'), DatabaseMetadata(name='performance_schema', table_count=9, status='ready'))))

setattr(list_databases_sample, '__module__', "dex_connectors.taosdata.taosdatamock")


class Mock:

    def __init__(self, connection_name: str = "taosdata") -> None:
        self.list_databases_script: Query[ListDatabasesInput, ListDatabasesOutput] = Query(LIST_DATABASES_DEFINITION)
        self.list_databases_script.default(list_databases_sample())
        self._connection = new_connection_with_operations(
            list_databases=self.list_databases_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.taosdata.taosdatamock")

__all__ = [
    "Mock",
    "list_databases_listed",
    "list_databases_provider_rejected",
    "list_databases_invalid_response",
    "list_databases_defect",
    "list_databases_retry",
    "list_databases_sample",
]
