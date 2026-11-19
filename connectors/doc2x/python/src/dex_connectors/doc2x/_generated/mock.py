# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetParseStatusInput, GetParseStatusOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_PARSE_STATUS_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_parse_status_found(value: GetParseStatusOutput, failure: Failure | None = None) -> Case[GetParseStatusOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_parse_status_found, '__module__', "dex_connectors.doc2x.doc2xmock")

def get_parse_status_provider_rejected(value: GetParseStatusOutput, failure: Failure | None = None) -> Case[GetParseStatusOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_parse_status_provider_rejected, '__module__', "dex_connectors.doc2x.doc2xmock")

def get_parse_status_invalid_response(value: GetParseStatusOutput, failure: Failure | None = None) -> Case[GetParseStatusOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_parse_status_invalid_response, '__module__', "dex_connectors.doc2x.doc2xmock")

def get_parse_status_defect(value: GetParseStatusOutput, failure: Failure | None = None) -> Case[GetParseStatusOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_parse_status_defect, '__module__', "dex_connectors.doc2x.doc2xmock")

def get_parse_status_retry(failure: Failure, after_seconds: float = 0) -> Case[GetParseStatusOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_parse_status_retry, '__module__', "dex_connectors.doc2x.doc2xmock")

def get_parse_status_processing() -> Case[GetParseStatusOutput]:
    return Case.branch_result(FOUND, GetParseStatusOutput(status='processing', progress=50))

setattr(get_parse_status_processing, '__module__', "dex_connectors.doc2x.doc2xmock")

def get_parse_status_completed() -> Case[GetParseStatusOutput]:
    return Case.branch_result(FOUND, GetParseStatusOutput(status='success', progress=100))

setattr(get_parse_status_completed, '__module__', "dex_connectors.doc2x.doc2xmock")


class Mock:

    def __init__(self, connection_name: str = "doc2x") -> None:
        self.get_parse_status_script: Query[GetParseStatusInput, GetParseStatusOutput] = Query(GET_PARSE_STATUS_DEFINITION)
        self.get_parse_status_script.default(get_parse_status_processing())
        self._connection = new_connection_with_operations(
            get_parse_status=self.get_parse_status_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.doc2x.doc2xmock")

__all__ = [
    "Mock",
    "get_parse_status_found",
    "get_parse_status_provider_rejected",
    "get_parse_status_invalid_response",
    "get_parse_status_defect",
    "get_parse_status_retry",
    "get_parse_status_processing",
    "get_parse_status_completed",
]
