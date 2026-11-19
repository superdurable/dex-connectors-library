# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read oppo through one durable provider Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.oppo import Connection, GetPushQuotaInput, GetPushQuotaOutput, new_get_push_quota_step

CONNECTION_NAME = "oppo"
_OUTCOME = dex.Attribute("oppo-get_push_quota-outcome", GetPushQuotaOutput)


def map_to_operation_input(input: GetPushQuotaInput) -> GetPushQuotaInput:
    """Forward the typed Flow input to the provider Query."""
    return input


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Begin the oppo read with the configured connection."
class _Start(dex.Step[GetPushQuotaInput]):
    def get_step_type(self) -> str:
        return "StartGetPushQuota"

    def execute(self, context: dex.Context, input: GetPushQuotaInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("GetPushQuotaQuery"), input)


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _Complete(dex.Step[QueryResult[GetPushQuotaOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetPushQuota"

    def execute(self, context: dex.Context, input: QueryResult[GetPushQuotaOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetPushQuotaFlow(dex.Flow[GetPushQuotaInput]):
    """Durably read oppo with its static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("oppo Flow requires its static connection")
        self._start = _Start()
        self._complete = _Complete()
        self._query = new_get_push_quota_step(
            step_type="GetPushQuotaQuery",
            step_input_type=GetPushQuotaInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "oppo",
                "explanation": "Call oppo getPushQuota.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity shown in Dex Web."""
        return "OppoGetPushQuota"

    def get_steps(self) -> dex.StepList[GetPushQuotaInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable provider result Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
