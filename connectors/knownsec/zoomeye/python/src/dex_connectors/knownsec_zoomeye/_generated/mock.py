# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetAccountQuotaInput, GetAccountQuotaOutput
from .definitions import CONNECTOR_ID, DEFECT, FETCHED, GET_ACCOUNT_QUOTA_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_account_quota_fetched(value: GetAccountQuotaOutput, failure: Failure | None = None) -> Case[GetAccountQuotaOutput]:
    return Case.branch_result(FETCHED, value, failure)

setattr(get_account_quota_fetched, '__module__', "dex_connectors.knownsec_zoomeye.zoomeyemock")

def get_account_quota_provider_rejected(value: GetAccountQuotaOutput, failure: Failure | None = None) -> Case[GetAccountQuotaOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_account_quota_provider_rejected, '__module__', "dex_connectors.knownsec_zoomeye.zoomeyemock")

def get_account_quota_invalid_response(value: GetAccountQuotaOutput, failure: Failure | None = None) -> Case[GetAccountQuotaOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_account_quota_invalid_response, '__module__', "dex_connectors.knownsec_zoomeye.zoomeyemock")

def get_account_quota_defect(value: GetAccountQuotaOutput, failure: Failure | None = None) -> Case[GetAccountQuotaOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_account_quota_defect, '__module__', "dex_connectors.knownsec_zoomeye.zoomeyemock")

def get_account_quota_retry(failure: Failure, after_seconds: float = 0) -> Case[GetAccountQuotaOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_account_quota_retry, '__module__', "dex_connectors.knownsec_zoomeye.zoomeyemock")

def get_account_quota_sample_quota() -> Case[GetAccountQuotaOutput]:
    return Case.branch_result(FETCHED, GetAccountQuotaOutput(plan='Premium', general_points='30000', zoomeye_points='10000000'))

setattr(get_account_quota_sample_quota, '__module__', "dex_connectors.knownsec_zoomeye.zoomeyemock")


class Mock:

    def __init__(self, connection_name: str = "zoomeye") -> None:
        self.get_account_quota_script: Query[GetAccountQuotaInput, GetAccountQuotaOutput] = Query(GET_ACCOUNT_QUOTA_DEFINITION)
        self.get_account_quota_script.default(get_account_quota_sample_quota())
        self._connection = new_connection_with_operations(
            get_account_quota=self.get_account_quota_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.knownsec_zoomeye.zoomeyemock")

__all__ = [
    "Mock",
    "get_account_quota_fetched",
    "get_account_quota_provider_rejected",
    "get_account_quota_invalid_response",
    "get_account_quota_defect",
    "get_account_quota_retry",
    "get_account_quota_sample_quota",
]
