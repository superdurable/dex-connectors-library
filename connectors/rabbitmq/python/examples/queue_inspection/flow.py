# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable RabbitMQ queue inspection Flow using getQueueMetrics."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.rabbitmq import (
    Connection,
    GetQueueMetricsInput,
    QueueMetrics,
    new_get_queue_metrics_step,
)

FLOW_TYPE = "RabbitMQQueueInspectionPython"
CONNECTION_NAME = "rabbitmq"
_CALL_STEP_TYPE = "CallGetQueueMetrics"
_RESULT = dex.Attribute("rabbitmq-queue-inspection", QueueMetrics)


def map_to_operation_input(request: GetQueueMetricsInput) -> GetQueueMetricsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:rabbitmq group-label:"RabbitMQ"
# dex:explanation text:"Start the RabbitMQ request."
class _StartRequest(dex.Step[GetQueueMetricsInput]):
    def get_step_type(self) -> str:
        return "StartRabbitMQRequest"

    def execute(self, context: dex.Context, input: GetQueueMetricsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetQueueMetrics"), input)


# dex:group group-id:rabbitmq group-label:"RabbitMQ"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[QueueMetrics]]):
    def get_step_type(self) -> str:
        return "CompleteRabbitMQRequest"

    def execute(self, context: dex.Context, input: QueryResult[QueueMetrics]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class QueueInspectionFlow(dex.Flow[GetQueueMetricsInput]):
    """Read queue metrics through the static rabbitmq connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("queue inspection requires the static rabbitmq connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_queue_metrics_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetQueueMetricsInput,
            annotations={
                "groupId": "rabbitmq",
                "groupLabel": "RabbitMQ",
                "explanation": "Read current counts for one RabbitMQ queue.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "RabbitMQQueueInspectionPython"

    def get_steps(self) -> dex.StepList[GetQueueMetricsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
