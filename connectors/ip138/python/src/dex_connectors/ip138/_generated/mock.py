# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetTokenUsageInput, GetTokenUsageOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_TOKEN_USAGE_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_token_usage_found(value: GetTokenUsageOutput, failure: Failure | None = None) -> Case[GetTokenUsageOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_token_usage_found, '__module__', "dex_connectors.ip138.ip138mock")

def get_token_usage_provider_rejected(value: GetTokenUsageOutput, failure: Failure | None = None) -> Case[GetTokenUsageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_token_usage_provider_rejected, '__module__', "dex_connectors.ip138.ip138mock")

def get_token_usage_invalid_response(value: GetTokenUsageOutput, failure: Failure | None = None) -> Case[GetTokenUsageOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_token_usage_invalid_response, '__module__', "dex_connectors.ip138.ip138mock")

def get_token_usage_defect(value: GetTokenUsageOutput, failure: Failure | None = None) -> Case[GetTokenUsageOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_token_usage_defect, '__module__', "dex_connectors.ip138.ip138mock")

def get_token_usage_retry(failure: Failure, after_seconds: float = 0) -> Case[GetTokenUsageOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_token_usage_retry, '__module__', "dex_connectors.ip138.ip138mock")

def get_token_usage_available_quota() -> Case[GetTokenUsageOutput]:
    return Case.branch_result(FOUND, GetTokenUsageOutput(current_hour_requests=10, hourly_allowance=3000, package_quota=19767))

setattr(get_token_usage_available_quota, '__module__', "dex_connectors.ip138.ip138mock")


class Mock:

    def __init__(self, connection_name: str = "ip138") -> None:
        self.get_token_usage_script: Query[GetTokenUsageInput, GetTokenUsageOutput] = Query(GET_TOKEN_USAGE_DEFINITION)
        self.get_token_usage_script.default(get_token_usage_available_quota())
        self._connection = new_connection_with_operations(
            get_token_usage=self.get_token_usage_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.ip138.ip138mock")

__all__ = [
    "Mock",
    "get_token_usage_found",
    "get_token_usage_provider_rejected",
    "get_token_usage_invalid_response",
    "get_token_usage_defect",
    "get_token_usage_retry",
    "get_token_usage_available_quota",
]
