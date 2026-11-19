# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetPushQuotaInput, GetPushQuotaOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_PUSH_QUOTA_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_push_quota_found(value: GetPushQuotaOutput, failure: Failure | None = None) -> Case[GetPushQuotaOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_push_quota_found, '__module__', "dex_connectors.oppo.oppomock")

def get_push_quota_provider_rejected(value: GetPushQuotaOutput, failure: Failure | None = None) -> Case[GetPushQuotaOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_push_quota_provider_rejected, '__module__', "dex_connectors.oppo.oppomock")

def get_push_quota_invalid_response(value: GetPushQuotaOutput, failure: Failure | None = None) -> Case[GetPushQuotaOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_push_quota_invalid_response, '__module__', "dex_connectors.oppo.oppomock")

def get_push_quota_defect(value: GetPushQuotaOutput, failure: Failure | None = None) -> Case[GetPushQuotaOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_push_quota_defect, '__module__', "dex_connectors.oppo.oppomock")

def get_push_quota_retry(failure: Failure, after_seconds: float = 0) -> Case[GetPushQuotaOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_push_quota_retry, '__module__', "dex_connectors.oppo.oppomock")

def get_push_quota_quota() -> Case[GetPushQuotaOutput]:
    return Case.branch_result(FOUND, GetPushQuotaOutput(total_count=100000, push_count=10000, remain_count=90000))

setattr(get_push_quota_quota, '__module__', "dex_connectors.oppo.oppomock")


class Mock:

    def __init__(self, connection_name: str = "oppo") -> None:
        self.get_push_quota_script: Query[GetPushQuotaInput, GetPushQuotaOutput] = Query(GET_PUSH_QUOTA_DEFINITION)
        self.get_push_quota_script.default(get_push_quota_quota())
        self._connection = new_connection_with_operations(
            get_push_quota=self.get_push_quota_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.oppo.oppomock")

__all__ = [
    "Mock",
    "get_push_quota_found",
    "get_push_quota_provider_rejected",
    "get_push_quota_invalid_response",
    "get_push_quota_defect",
    "get_push_quota_retry",
    "get_push_quota_quota",
]
