# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetAccountStatusInput, GetAccountStatusOutput
from .definitions import CONNECTOR_ID, DEFECT, GET_ACCOUNT_STATUS_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED, RETRIEVED

def get_account_status_retrieved(value: GetAccountStatusOutput, failure: Failure | None = None) -> Case[GetAccountStatusOutput]:
    return Case.branch_result(RETRIEVED, value, failure)

setattr(get_account_status_retrieved, '__module__', "dex_connectors.istero.isteromock")

def get_account_status_provider_rejected(value: GetAccountStatusOutput, failure: Failure | None = None) -> Case[GetAccountStatusOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_account_status_provider_rejected, '__module__', "dex_connectors.istero.isteromock")

def get_account_status_invalid_response(value: GetAccountStatusOutput, failure: Failure | None = None) -> Case[GetAccountStatusOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_account_status_invalid_response, '__module__', "dex_connectors.istero.isteromock")

def get_account_status_defect(value: GetAccountStatusOutput, failure: Failure | None = None) -> Case[GetAccountStatusOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_account_status_defect, '__module__', "dex_connectors.istero.isteromock")

def get_account_status_retry(failure: Failure, after_seconds: float = 0) -> Case[GetAccountStatusOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_account_status_retry, '__module__', "dex_connectors.istero.isteromock")

def get_account_status_sample_account_status() -> Case[GetAccountStatusOutput]:
    return Case.branch_result(RETRIEVED, GetAccountStatusOutput(isid=60066023, today_used=3, today_remaining=997))

setattr(get_account_status_sample_account_status, '__module__', "dex_connectors.istero.isteromock")


class Mock:

    def __init__(self, connection_name: str = "istero") -> None:
        self.get_account_status_script: Query[GetAccountStatusInput, GetAccountStatusOutput] = Query(GET_ACCOUNT_STATUS_DEFINITION)
        self.get_account_status_script.default(get_account_status_sample_account_status())
        self._connection = new_connection_with_operations(
            get_account_status=self.get_account_status_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.istero.isteromock")

__all__ = [
    "Mock",
    "get_account_status_retrieved",
    "get_account_status_provider_rejected",
    "get_account_status_invalid_response",
    "get_account_status_defect",
    "get_account_status_retry",
    "get_account_status_sample_account_status",
]
