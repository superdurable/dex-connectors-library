# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetQueueMetricsInput, QueueMetrics
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_QUEUE_METRICS_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_queue_metrics_found(value: QueueMetrics, failure: Failure | None = None) -> Case[QueueMetrics]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_queue_metrics_found, '__module__', "dex_connectors.rabbitmq.rabbitmqmock")

def get_queue_metrics_not_found(value: QueueMetrics, failure: Failure | None = None) -> Case[QueueMetrics]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_queue_metrics_not_found, '__module__', "dex_connectors.rabbitmq.rabbitmqmock")

def get_queue_metrics_provider_rejected(value: QueueMetrics, failure: Failure | None = None) -> Case[QueueMetrics]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_queue_metrics_provider_rejected, '__module__', "dex_connectors.rabbitmq.rabbitmqmock")

def get_queue_metrics_invalid_response(value: QueueMetrics, failure: Failure | None = None) -> Case[QueueMetrics]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_queue_metrics_invalid_response, '__module__', "dex_connectors.rabbitmq.rabbitmqmock")

def get_queue_metrics_defect(value: QueueMetrics, failure: Failure | None = None) -> Case[QueueMetrics]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_queue_metrics_defect, '__module__', "dex_connectors.rabbitmq.rabbitmqmock")

def get_queue_metrics_retry(failure: Failure, after_seconds: float = 0) -> Case[QueueMetrics]:
    return Case.retry(failure, after_seconds)

setattr(get_queue_metrics_retry, '__module__', "dex_connectors.rabbitmq.rabbitmqmock")

def get_queue_metrics_found_default() -> Case[QueueMetrics]:
    return Case.branch_result(FOUND, QueueMetrics(virtual_host='/', name='work', messages=3, consumers=1, state='running'))

setattr(get_queue_metrics_found_default, '__module__', "dex_connectors.rabbitmq.rabbitmqmock")


class Mock:

    def __init__(self, connection_name: str = "rabbitmq") -> None:
        self.get_queue_metrics_script: Query[GetQueueMetricsInput, QueueMetrics] = Query(GET_QUEUE_METRICS_DEFINITION)
        self.get_queue_metrics_script.default(get_queue_metrics_found_default())
        self._connection = new_connection_with_operations(
            get_queue_metrics=self.get_queue_metrics_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.rabbitmq.rabbitmqmock")

__all__ = [
    "Mock",
    "get_queue_metrics_found",
    "get_queue_metrics_not_found",
    "get_queue_metrics_provider_rejected",
    "get_queue_metrics_invalid_response",
    "get_queue_metrics_defect",
    "get_queue_metrics_retry",
    "get_queue_metrics_found_default",
]
