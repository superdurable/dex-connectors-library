# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Zhipu generateText Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.zhipu import (
    Connection,
    GenerateTextInput,
    GenerateTextOutput,
    new_generate_text_step,
)

CONNECTION_NAME = "zhipu"
_RESULT = dex.Attribute("zhipu-result", GenerateTextOutput)


def map_to_operation_input(request: GenerateTextInput) -> GenerateTextInput:
    """Forward the provider request."""
    return request


# dex:group group-id:zhipu group-label:"Zhipu"
# dex:explanation text:"Start the Zhipu query."
class _StartQuery(dex.Step[GenerateTextInput]):
    def execute(self, context: dex.Context, input: GenerateTextInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:zhipu group-label:"Zhipu"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[GenerateTextOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[GenerateTextOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GenerateTextFlow(dex.Flow[GenerateTextInput]):
    """Run one Zhipu query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Zhipu Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_generate_text_step(
            step_type="CallProvider",
            step_input_type=GenerateTextInput,
            annotations={
                "groupId": "zhipu",
                "groupLabel": "Zhipu",
                "explanation": "Run Zhipu generateText.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            generated=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "GenerateTextZhipu"

    def get_steps(self) -> dex.StepList[GenerateTextInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
