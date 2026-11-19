# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one PDF parse task through a statically named Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.doc2x import (
    Connection,
    GetParseStatusInput,
    GetParseStatusOutput,
    new_get_parse_status_step,
)

CONNECTION_NAME = "doc2x"
_OUTCOME = dex.Attribute("doc2x-get-parse-status-outcome", GetParseStatusOutput)


def map_to_operation_input(input: GetParseStatusInput) -> GetParseStatusInput:
    """Pass the existing task ID to the provider Query."""
    return input


# dex:group group-id:get-parse-status group-label:"Get parse status"
# dex:explanation text:"Start the Doc2X PDF parse task read."
class _StartRequest(dex.Step[GetParseStatusInput]):
    def get_step_type(self) -> str:
        return "StartGetParseStatus"

    def execute(self, context: dex.Context, input: GetParseStatusInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetParseStatus"), input)


# dex:group group-id:get-parse-status group-label:"Get parse status"
# dex:explanation text:"Persist parse status, then complete the Flow."
class _CompleteResult(dex.Step[QueryResult[GetParseStatusOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetParseStatus"

    def execute(self, context: dex.Context, input: QueryResult[GetParseStatusOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetParseStatusFlow(dex.Flow[GetParseStatusInput]):
    """Read a PDF parse task from the static `doc2x` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("parse status query requires the static doc2x connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._read = new_get_parse_status_step(
            step_type="CallGetParseStatus",
            step_input_type=GetParseStatusInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Doc2X",
                "explanation": "Call Doc2X's getParseStatus operation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable type shown in Dex Web."""
        return "Doc2XGetParseStatus"

    def get_steps(self) -> dex.StepList[GetParseStatusInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the returned state and completion percentage."""
        return dex.PersistenceSchema.of(_OUTCOME)
