# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Marketo lead inspection Flow using getLead."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.adobe_marketo import (
    Connection,
    GetLeadInput,
    LeadProfile,
    new_get_lead_step,
)

FLOW_TYPE = "MarketoLeadInspectionPython"
CONNECTION_NAME = "adobe-marketo"
_CALL_STEP_TYPE = "CallMarketo"
_RESULT = dex.Attribute("adobe_marketo-lead", LeadProfile)


def map_to_operation_input(request: GetLeadInput) -> GetLeadInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:marketo group-label:"Marketo"
# dex:explanation text:"Start the Marketo request."
class _StartRequest(dex.Step[GetLeadInput]):
    def get_step_type(self) -> str:
        return "StartMarketoRequest"

    def execute(self, context: dex.Context, input: GetLeadInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallMarketo"), input)


# dex:group group-id:marketo group-label:"Marketo"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[LeadProfile]]):
    def get_step_type(self) -> str:
        return "CompleteMarketoRequest"

    def execute(self, context: dex.Context, input: QueryResult[LeadProfile]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class LeadInspectionFlow(dex.Flow[GetLeadInput]):
    """Read one lead through the static adobe-marketo connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("lead inspection requires the static adobe-marketo connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_lead_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetLeadInput,
            annotations={
                "groupId": "marketo",
                "groupLabel": "Marketo",
                "explanation": "Read one lead from the connected Marketo account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "MarketoLeadInspectionPython"

    def get_steps(self) -> dex.StepList[GetLeadInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
