# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Lever opportunity inspection Flow using getOpportunity."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.lever import (
    Connection,
    GetOpportunityInput,
    OpportunityStage,
    new_get_opportunity_step,
)

FLOW_TYPE = "LeverOpportunityInspectionPython"
CONNECTION_NAME = "lever"
_CALL_STEP_TYPE = "CallLever"
_RESULT = dex.Attribute("lever-opportunity", OpportunityStage)


def map_to_operation_input(request: GetOpportunityInput) -> GetOpportunityInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:lever group-label:"Lever"
# dex:explanation text:"Start the Lever request."
class _StartRequest(dex.Step[GetOpportunityInput]):
    def get_step_type(self) -> str:
        return "StartLeverRequest"

    def execute(self, context: dex.Context, input: GetOpportunityInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallLever"), input)


# dex:group group-id:lever group-label:"Lever"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[OpportunityStage]]):
    def get_step_type(self) -> str:
        return "CompleteLeverRequest"

    def execute(self, context: dex.Context, input: QueryResult[OpportunityStage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class OpportunityInspectionFlow(dex.Flow[GetOpportunityInput]):
    """Read one opportunity through the static lever connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("opportunity inspection requires the static lever connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_opportunity_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetOpportunityInput,
            annotations={
                "groupId": "lever",
                "groupLabel": "Lever",
                "explanation": "Read one opportunity from the connected Lever account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "LeverOpportunityInspectionPython"

    def get_steps(self) -> dex.StepList[GetOpportunityInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
