# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import timedelta

from dex_connectors.sdk import (
    BranchDefinition,
    MutationDefinition,
    OperationRef,
    QueryDefinition,
    RetryPolicy,
    StepDefaults,
    default_connector_step_execute_retry_policy,
)

CONNECTOR_ID = "rabbitmq"
PROVIDER = "rabbitmq"
GET_QUEUE_METRICS_OPERATION_ID = "getQueueMetrics"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_QUEUE_METRICS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_QUEUE_METRICS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "RabbitMQ returned the queue metrics.", False),
        BranchDefinition(NOT_FOUND, "The queue or virtual host does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "RabbitMQ conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "RabbitMQ returned malformed or oversized queue data.", True),
        BranchDefinition(DEFECT, "Invalid queue input or unavailable credentials.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
