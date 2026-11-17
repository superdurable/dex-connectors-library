# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one signing flow through a statically named Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.esign import (
    Connection,
    GetSignFlowStatusInput,
    GetSignFlowStatusOutput,
    new_get_sign_flow_status_step,
)

CONNECTION_NAME = "esign"
_OUTCOME = dex.Attribute("esign-get-sign-flow-status-outcome", GetSignFlowStatusOutput)


def map_to_operation_input(input: GetSignFlowStatusInput) -> GetSignFlowStatusInput:
    """Pass the existing signing flow ID to the provider Query."""
    return input


# dex:group group-id:get-sign-flow-status group-label:"Get sign flow status"
# dex:explanation text:"Start the eSign signing flow read."
class _StartRequest(dex.Step[GetSignFlowStatusInput]):
    def get_step_type(self) -> str:
        return "StartGetSignFlowStatus"

    def execute(self, context: dex.Context, input: GetSignFlowStatusInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetSignFlowStatus"), input)


# dex:group group-id:get-sign-flow-status group-label:"Get sign flow status"
# dex:explanation text:"Persist signing status, then complete the Flow."
class _CompleteResult(dex.Step[QueryResult[GetSignFlowStatusOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetSignFlowStatus"

    def execute(self, context: dex.Context, input: QueryResult[GetSignFlowStatusOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetSignFlowStatusFlow(dex.Flow[GetSignFlowStatusInput]):
    """Read a signing flow from the static `esign` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("signing status query requires the static esign connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._read = new_get_sign_flow_status_step(
            step_type="CallGetSignFlowStatus",
            step_input_type=GetSignFlowStatusInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "eSign",
                "explanation": "Call eSign's getSignFlowStatus operation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable type shown in Dex Web."""
        return "eSignGetSignFlowStatus"

    def get_steps(self) -> dex.StepList[GetSignFlowStatusInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the requested flow ID and numeric signing status."""
        return dex.PersistenceSchema.of(_OUTCOME)
