# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Datadog lookup Flow using the queryMetrics Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.datadog import Connection, MetricsQuery, QueryMetricsInput, new_query_metrics_step

FLOW_TYPE = "DatadogLookup"
CONNECTION_NAME = "datadog"
_OPERATION_STEP_TYPE = "QueryMetricsDatadog"
_REQUEST = dex.Attribute("datadog-lookup-request", QueryMetricsInput)
_OUTPUT = dex.Attribute("datadog-lookup-output", MetricsQuery)


def map_to_operation_input(request: QueryMetricsInput) -> QueryMetricsInput:
    """Query the requested metric interval unchanged."""
    return request


# dex:group group-id:datadog-lookup group-label:"Datadog lookup"
# dex:explanation text:"Record the lookup request before querying Datadog metrics."
class _StartLookup(dex.Step[QueryMetricsInput]):
    def get_step_type(self) -> str:
        return "StartDatadogLookup"

    def execute(self, context: dex.Context, input: QueryMetricsInput) -> dex.StepDecision:
        _REQUEST.set(context, input)
        return dex.go_to(dex.StepRef(_OPERATION_STEP_TYPE), input)


# dex:group group-id:datadog-lookup group-label:"Datadog lookup"
# dex:explanation text:"Record the metric series and complete the Flow."
class _CompleteLookup(dex.Step[QueryResult[MetricsQuery]]):
    def get_step_type(self) -> str:
        return "CompleteDatadogLookup"

    def execute(self, context: dex.Context, input: QueryResult[MetricsQuery]) -> dex.StepDecision:
        _OUTPUT.set(context, input.value)
        return dex.graceful_complete(input.value)


class DatadogLookupFlow(dex.Flow[QueryMetricsInput]):
    """Query Datadog metrics through the static datadog connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Datadog lookup requires the static datadog connection")
        self._start = _StartLookup()
        self._complete = _CompleteLookup()
        self._list = new_query_metrics_step(
            step_type=_OPERATION_STEP_TYPE,
            step_input_type=QueryMetricsInput,
            annotations={
                "groupId": "datadog",
                "groupLabel": "Datadog",
                "explanation": "Query Datadog timeseries points over an explicit Unix-second interval.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            queried=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[QueryMetricsInput]:
        """Register the start, provider list, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._list, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the request and metric query result."""
        return dex.PersistenceSchema.of(_REQUEST, _OUTPUT)
