# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetOnlineVersionInput, GetOnlineVersionOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_ONLINE_VERSION_DEFINITION, INVALID_RESPONSE, NO_DATA, PROVIDER_REJECTED

def get_online_version_found(value: GetOnlineVersionOutput, failure: Failure | None = None) -> Case[GetOnlineVersionOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_online_version_found, '__module__', "dex_connectors.vivo.vivomock")

def get_online_version_no_data(value: GetOnlineVersionOutput, failure: Failure | None = None) -> Case[GetOnlineVersionOutput]:
    return Case.branch_result(NO_DATA, value, failure)

setattr(get_online_version_no_data, '__module__', "dex_connectors.vivo.vivomock")

def get_online_version_provider_rejected(value: GetOnlineVersionOutput, failure: Failure | None = None) -> Case[GetOnlineVersionOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_online_version_provider_rejected, '__module__', "dex_connectors.vivo.vivomock")

def get_online_version_invalid_response(value: GetOnlineVersionOutput, failure: Failure | None = None) -> Case[GetOnlineVersionOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_online_version_invalid_response, '__module__', "dex_connectors.vivo.vivomock")

def get_online_version_defect(value: GetOnlineVersionOutput, failure: Failure | None = None) -> Case[GetOnlineVersionOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_online_version_defect, '__module__', "dex_connectors.vivo.vivomock")

def get_online_version_retry(failure: Failure, after_seconds: float = 0) -> Case[GetOnlineVersionOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_online_version_retry, '__module__', "dex_connectors.vivo.vivomock")

def get_online_version_online_version() -> Case[GetOnlineVersionOutput]:
    return Case.branch_result(FOUND, GetOnlineVersionOutput(package_name='com.example.app', version_name='3.2.1', version_code=100))

setattr(get_online_version_online_version, '__module__', "dex_connectors.vivo.vivomock")

def get_online_version_no_app() -> Case[GetOnlineVersionOutput]:
    return Case.branch_result(NO_DATA, GetOnlineVersionOutput(package_name='com.example.app', version_name='', version_code=0))

setattr(get_online_version_no_app, '__module__', "dex_connectors.vivo.vivomock")


class Mock:

    def __init__(self, connection_name: str = "vivo") -> None:
        self.get_online_version_script: Query[GetOnlineVersionInput, GetOnlineVersionOutput] = Query(GET_ONLINE_VERSION_DEFINITION)
        self.get_online_version_script.default(get_online_version_online_version())
        self._connection = new_connection_with_operations(
            get_online_version=self.get_online_version_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.vivo.vivomock")

__all__ = [
    "Mock",
    "get_online_version_found",
    "get_online_version_no_data",
    "get_online_version_provider_rejected",
    "get_online_version_invalid_response",
    "get_online_version_defect",
    "get_online_version_retry",
    "get_online_version_online_version",
    "get_online_version_no_app",
]
