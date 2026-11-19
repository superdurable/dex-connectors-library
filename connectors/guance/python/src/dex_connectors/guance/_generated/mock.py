# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import DashboardMetadata, ListDashboardsPageInput, ListDashboardsPageOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_DASHBOARDS_PAGE_DEFINITION, PROVIDER_REJECTED

def list_dashboards_page_listed(value: ListDashboardsPageOutput, failure: Failure | None = None) -> Case[ListDashboardsPageOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_dashboards_page_listed, '__module__', "dex_connectors.guance.guancemock")

def list_dashboards_page_provider_rejected(value: ListDashboardsPageOutput, failure: Failure | None = None) -> Case[ListDashboardsPageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_dashboards_page_provider_rejected, '__module__', "dex_connectors.guance.guancemock")

def list_dashboards_page_invalid_response(value: ListDashboardsPageOutput, failure: Failure | None = None) -> Case[ListDashboardsPageOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_dashboards_page_invalid_response, '__module__', "dex_connectors.guance.guancemock")

def list_dashboards_page_defect(value: ListDashboardsPageOutput, failure: Failure | None = None) -> Case[ListDashboardsPageOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_dashboards_page_defect, '__module__', "dex_connectors.guance.guancemock")

def list_dashboards_page_retry(failure: Failure, after_seconds: float = 0) -> Case[ListDashboardsPageOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_dashboards_page_retry, '__module__', "dex_connectors.guance.guancemock")

def list_dashboards_page_one_dashboard() -> Case[ListDashboardsPageOutput]:
    return Case.branch_result(LISTED, ListDashboardsPageOutput(total_count=1, page_index=1, page_size=10, dashboards=(DashboardMetadata(uuid='dsbd_example', name='Operations'),)))

setattr(list_dashboards_page_one_dashboard, '__module__', "dex_connectors.guance.guancemock")


class Mock:

    def __init__(self, connection_name: str = "guance") -> None:
        self.list_dashboards_page_script: Query[ListDashboardsPageInput, ListDashboardsPageOutput] = Query(LIST_DASHBOARDS_PAGE_DEFINITION)
        self.list_dashboards_page_script.default(list_dashboards_page_one_dashboard())
        self._connection = new_connection_with_operations(
            list_dashboards_page=self.list_dashboards_page_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.guance.guancemock")

__all__ = [
    "Mock",
    "list_dashboards_page_listed",
    "list_dashboards_page_provider_rejected",
    "list_dashboards_page_invalid_response",
    "list_dashboards_page_defect",
    "list_dashboards_page_retry",
    "list_dashboards_page_one_dashboard",
]
