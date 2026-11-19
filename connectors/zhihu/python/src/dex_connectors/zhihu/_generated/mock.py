# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListQuotasInput, Quota, QuotaPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_QUOTAS_DEFINITION, PROVIDER_REJECTED

def list_quotas_listed(value: QuotaPage, failure: Failure | None = None) -> Case[QuotaPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_quotas_listed, '__module__', "dex_connectors.zhihu.zhihumock")

def list_quotas_provider_rejected(value: QuotaPage, failure: Failure | None = None) -> Case[QuotaPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_quotas_provider_rejected, '__module__', "dex_connectors.zhihu.zhihumock")

def list_quotas_invalid_response(value: QuotaPage, failure: Failure | None = None) -> Case[QuotaPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_quotas_invalid_response, '__module__', "dex_connectors.zhihu.zhihumock")

def list_quotas_defect(value: QuotaPage, failure: Failure | None = None) -> Case[QuotaPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_quotas_defect, '__module__', "dex_connectors.zhihu.zhihumock")

def list_quotas_retry(failure: Failure, after_seconds: float = 0) -> Case[QuotaPage]:
    return Case.retry(failure, after_seconds)

setattr(list_quotas_retry, '__module__', "dex_connectors.zhihu.zhihumock")

def list_quotas_listed_quota() -> Case[QuotaPage]:
    return Case.branch_result(LISTED, QuotaPage(quotas=(Quota(api_id='knowledge', api_name='Knowledge Base', total_quota=500, total_used=12, remaining_quota=488),)))

setattr(list_quotas_listed_quota, '__module__', "dex_connectors.zhihu.zhihumock")


class Mock:

    def __init__(self, connection_name: str = "zhihu") -> None:
        self.list_quotas_script: Query[ListQuotasInput, QuotaPage] = Query(LIST_QUOTAS_DEFINITION)
        self.list_quotas_script.default(list_quotas_listed_quota())
        self._connection = new_connection_with_operations(
            list_quotas=self.list_quotas_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.zhihu.zhihumock")

__all__ = [
    "Mock",
    "list_quotas_listed",
    "list_quotas_provider_rejected",
    "list_quotas_invalid_response",
    "list_quotas_defect",
    "list_quotas_retry",
    "list_quotas_listed_quota",
]
