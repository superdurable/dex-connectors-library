# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AccessClientResult, GetAccessClientInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_ACCESS_CLIENT_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_access_client_found(value: AccessClientResult, failure: Failure | None = None) -> Case[AccessClientResult]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_access_client_found, '__module__', "dex_connectors.gowu.gowumock")

def get_access_client_provider_rejected(value: AccessClientResult, failure: Failure | None = None) -> Case[AccessClientResult]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_access_client_provider_rejected, '__module__', "dex_connectors.gowu.gowumock")

def get_access_client_invalid_response(value: AccessClientResult, failure: Failure | None = None) -> Case[AccessClientResult]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_access_client_invalid_response, '__module__', "dex_connectors.gowu.gowumock")

def get_access_client_defect(value: AccessClientResult, failure: Failure | None = None) -> Case[AccessClientResult]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_access_client_defect, '__module__', "dex_connectors.gowu.gowumock")

def get_access_client_retry(failure: Failure, after_seconds: float = 0) -> Case[AccessClientResult]:
    return Case.retry(failure, after_seconds)

setattr(get_access_client_retry, '__module__', "dex_connectors.gowu.gowumock")

def get_access_client_found_app() -> Case[AccessClientResult]:
    return Case.branch_result(FOUND, AccessClientResult(remark='Example integration', api_call_count=42))

setattr(get_access_client_found_app, '__module__', "dex_connectors.gowu.gowumock")


class Mock:

    def __init__(self, connection_name: str = "gowu") -> None:
        self.get_access_client_script: Query[GetAccessClientInput, AccessClientResult] = Query(GET_ACCESS_CLIENT_DEFINITION)
        self.get_access_client_script.default(get_access_client_found_app())
        self._connection = new_connection_with_operations(
            get_access_client=self.get_access_client_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.gowu.gowumock")

__all__ = [
    "Mock",
    "get_access_client_found",
    "get_access_client_provider_rejected",
    "get_access_client_invalid_response",
    "get_access_client_defect",
    "get_access_client_retry",
    "get_access_client_found_app",
]
