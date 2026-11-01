# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListMonitorsInput, MetricSeries, MetricsQuery, Monitor, MonitorsPage, QueryMetricsInput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_MONITORS_DEFINITION, PROVIDER_REJECTED, QUERIED, QUERY_METRICS_DEFINITION

def query_metrics_queried(value: MetricsQuery, failure: Failure | None = None) -> Case[MetricsQuery]:
    return Case.branch_result(QUERIED, value, failure)

setattr(query_metrics_queried, '__module__', "dex_connectors.datadog.datadogmock")

def query_metrics_provider_rejected(value: MetricsQuery, failure: Failure | None = None) -> Case[MetricsQuery]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(query_metrics_provider_rejected, '__module__', "dex_connectors.datadog.datadogmock")

def query_metrics_invalid_response(value: MetricsQuery, failure: Failure | None = None) -> Case[MetricsQuery]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(query_metrics_invalid_response, '__module__', "dex_connectors.datadog.datadogmock")

def query_metrics_defect(value: MetricsQuery, failure: Failure | None = None) -> Case[MetricsQuery]:
    return Case.branch_result(DEFECT, value, failure)

setattr(query_metrics_defect, '__module__', "dex_connectors.datadog.datadogmock")

def query_metrics_retry(failure: Failure, after_seconds: float = 0) -> Case[MetricsQuery]:
    return Case.retry(failure, after_seconds)

setattr(query_metrics_retry, '__module__', "dex_connectors.datadog.datadogmock")

def query_metrics_queried_default() -> Case[MetricsQuery]:
    return Case.branch_result(QUERIED, MetricsQuery(status='ok', series=(MetricSeries(metric='system.cpu.user', pointlist=((1761436800000, 0.23),)),)))

setattr(query_metrics_queried_default, '__module__', "dex_connectors.datadog.datadogmock")

def list_monitors_listed(value: MonitorsPage, failure: Failure | None = None) -> Case[MonitorsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_monitors_listed, '__module__', "dex_connectors.datadog.datadogmock")

def list_monitors_provider_rejected(value: MonitorsPage, failure: Failure | None = None) -> Case[MonitorsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_monitors_provider_rejected, '__module__', "dex_connectors.datadog.datadogmock")

def list_monitors_invalid_response(value: MonitorsPage, failure: Failure | None = None) -> Case[MonitorsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_monitors_invalid_response, '__module__', "dex_connectors.datadog.datadogmock")

def list_monitors_defect(value: MonitorsPage, failure: Failure | None = None) -> Case[MonitorsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_monitors_defect, '__module__', "dex_connectors.datadog.datadogmock")

def list_monitors_retry(failure: Failure, after_seconds: float = 0) -> Case[MonitorsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_monitors_retry, '__module__', "dex_connectors.datadog.datadogmock")

def list_monitors_listed_default() -> Case[MonitorsPage]:
    return Case.branch_result(LISTED, MonitorsPage(monitors=(Monitor(id=12345, name='Meridian API latency', type='metric alert', overall_state='OK'),)))

setattr(list_monitors_listed_default, '__module__', "dex_connectors.datadog.datadogmock")


class Mock:

    def __init__(self, connection_name: str = "datadog") -> None:
        self.query_metrics_script: Query[QueryMetricsInput, MetricsQuery] = Query(QUERY_METRICS_DEFINITION)
        self.query_metrics_script.default(query_metrics_queried_default())
        self.list_monitors_script: Query[ListMonitorsInput, MonitorsPage] = Query(LIST_MONITORS_DEFINITION)
        self.list_monitors_script.default(list_monitors_listed_default())
        self._connection = new_connection_with_operations(
            query_metrics=self.query_metrics_script,
            list_monitors=self.list_monitors_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.datadog.datadogmock")

__all__ = [
    "Mock",
    "query_metrics_queried",
    "query_metrics_provider_rejected",
    "query_metrics_invalid_response",
    "query_metrics_defect",
    "query_metrics_retry",
    "query_metrics_queried_default",
    "list_monitors_listed",
    "list_monitors_provider_rejected",
    "list_monitors_invalid_response",
    "list_monitors_defect",
    "list_monitors_retry",
    "list_monitors_listed_default",
]
