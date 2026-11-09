# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Creatio contact inspection Flow using getContact."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.creatio import (
    Connection,
    ContactSummary,
    GetContactInput,
    new_get_contact_step,
)

FLOW_TYPE = "CreatioContactInspectionPython"
CONNECTION_NAME = "creatio"
_CALL_STEP_TYPE = "CallCreatio"
_RESULT = dex.Attribute("creatio-contact", ContactSummary)


def map_to_operation_input(request: GetContactInput) -> GetContactInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:creatio group-label:"Creatio"
# dex:explanation text:"Start the Creatio request."
class _StartRequest(dex.Step[GetContactInput]):
    def get_step_type(self) -> str:
        return "StartCreatioRequest"

    def execute(self, context: dex.Context, input: GetContactInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallCreatio"), input)


# dex:group group-id:creatio group-label:"Creatio"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[ContactSummary]]):
    def get_step_type(self) -> str:
        return "CompleteCreatioRequest"

    def execute(self, context: dex.Context, input: QueryResult[ContactSummary]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ContactInspectionFlow(dex.Flow[GetContactInput]):
    """Read one contact through the static creatio connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("contact inspection requires the static creatio connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_contact_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetContactInput,
            annotations={
                "groupId": "creatio",
                "groupLabel": "Creatio",
                "explanation": "Read one contact from the connected Creatio account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "CreatioContactInspectionPython"

    def get_steps(self) -> dex.StepList[GetContactInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
