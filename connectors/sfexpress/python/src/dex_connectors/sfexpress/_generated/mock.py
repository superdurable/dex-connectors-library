# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import QueryRoutesInput, RouteEvent, RouteQueryResult
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, PROVIDER_REJECTED, QUERY_ROUTES_DEFINITION

def query_routes_found(value: RouteQueryResult, failure: Failure | None = None) -> Case[RouteQueryResult]:
    return Case.branch_result(FOUND, value, failure)

setattr(query_routes_found, '__module__', "dex_connectors.sfexpress.sfexpressmock")

def query_routes_provider_rejected(value: RouteQueryResult, failure: Failure | None = None) -> Case[RouteQueryResult]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(query_routes_provider_rejected, '__module__', "dex_connectors.sfexpress.sfexpressmock")

def query_routes_invalid_response(value: RouteQueryResult, failure: Failure | None = None) -> Case[RouteQueryResult]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(query_routes_invalid_response, '__module__', "dex_connectors.sfexpress.sfexpressmock")

def query_routes_defect(value: RouteQueryResult, failure: Failure | None = None) -> Case[RouteQueryResult]:
    return Case.branch_result(DEFECT, value, failure)

setattr(query_routes_defect, '__module__', "dex_connectors.sfexpress.sfexpressmock")

def query_routes_retry(failure: Failure, after_seconds: float = 0) -> Case[RouteQueryResult]:
    return Case.retry(failure, after_seconds)

setattr(query_routes_retry, '__module__', "dex_connectors.sfexpress.sfexpressmock")

def query_routes_found_route() -> Case[RouteQueryResult]:
    return Case.branch_result(FOUND, RouteQueryResult(waybill_number='SF1234567890123', routes=(RouteEvent(occurred_at='2026-11-13 09:30:00', opcode='50'),)))

setattr(query_routes_found_route, '__module__', "dex_connectors.sfexpress.sfexpressmock")


class Mock:

    def __init__(self, connection_name: str = "sfexpress") -> None:
        self.query_routes_script: Query[QueryRoutesInput, RouteQueryResult] = Query(QUERY_ROUTES_DEFINITION)
        self.query_routes_script.default(query_routes_found_route())
        self._connection = new_connection_with_operations(
            query_routes=self.query_routes_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.sfexpress.sfexpressmock")

__all__ = [
    "Mock",
    "query_routes_found",
    "query_routes_provider_rejected",
    "query_routes_invalid_response",
    "query_routes_defect",
    "query_routes_retry",
    "query_routes_found_route",
]
