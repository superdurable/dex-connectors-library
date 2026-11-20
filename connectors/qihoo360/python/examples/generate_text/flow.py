# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Qihoo360 generateText Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.qihoo360 import (
    Connection,
    GenerateTextInput,
    GenerateTextOutput,
    new_generate_text_step,
)

CONNECTION_NAME = "qihoo360"
_RESULT = dex.Attribute("qihoo360-result", GenerateTextOutput)


def map_to_operation_input(request: GenerateTextInput) -> GenerateTextInput:
    """Forward the provider request."""
    return request


# dex:group group-id:qihoo360 group-label:"Qihoo360"
# dex:explanation text:"Start the Qihoo360 query."
class _StartQuery(dex.Step[GenerateTextInput]):
    def execute(self, context: dex.Context, input: GenerateTextInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:qihoo360 group-label:"Qihoo360"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[GenerateTextOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[GenerateTextOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GenerateTextFlow(dex.Flow[GenerateTextInput]):
    """Run one Qihoo360 query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Qihoo360 Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_generate_text_step(
            step_type="CallProvider",
            step_input_type=GenerateTextInput,
            annotations={
                "groupId": "qihoo360",
                "groupLabel": "Qihoo360",
                "explanation": "Run Qihoo360 generateText.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            generated=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "GenerateTextQihoo360"

    def get_steps(self) -> dex.StepList[GenerateTextInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
