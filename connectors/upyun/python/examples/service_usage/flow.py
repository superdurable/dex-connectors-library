# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read upyun data through the static Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.upyun import Connection, GetServiceUsageInput, ServiceUsage, new_get_service_usage_step

CONNECTION_NAME = "upyun"
_OUTCOME = dex.Attribute("upyun-service-usage-result", ServiceUsage)


def map_to_operation_input(input: GetServiceUsageInput) -> GetServiceUsageInput:
    """Pass the typed request to the provider Query."""
    return input


# dex:group group-id:service-usage group-label:"ServiceUsage"
# dex:explanation text:"Start the upyun Query."
class _StartRequest(dex.Step[GetServiceUsageInput]):
    def get_step_type(self) -> str:
        return "StartServiceUsage"

    def execute(self, context: dex.Context, input: GetServiceUsageInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallServiceUsage"), input)


# dex:group group-id:service-usage group-label:"ServiceUsage"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[ServiceUsage]]):
    def get_step_type(self) -> str:
        return "CompleteServiceUsage"

    def execute(self, context: dex.Context, input: QueryResult[ServiceUsage]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ServiceUsageFlow(dex.Flow[GetServiceUsageInput]):
    """Run getServiceUsage using the static `upyun` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("upyun example requires the static connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._query = new_get_service_usage_step(
            step_type="CallServiceUsage",
            step_input_type=GetServiceUsageInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "upyun",
                "explanation": "Call the upyun getServiceUsage Query.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            read=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "UpyunServiceUsage"

    def get_steps(self) -> dex.StepList[GetServiceUsageInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the typed provider output."""
        return dex.PersistenceSchema.of(_OUTCOME)
