# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Niutrans translateText Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.niutrans import (
    Connection,
    TranslateTextInput,
    TranslateTextOutput,
    new_translate_text_step,
)

CONNECTION_NAME = "niutrans"
_RESULT = dex.Attribute("niutrans-result", TranslateTextOutput)


def map_to_operation_input(request: TranslateTextInput) -> TranslateTextInput:
    """Forward the provider request."""
    return request


# dex:group group-id:niutrans group-label:"Niutrans"
# dex:explanation text:"Start the Niutrans query."
class _StartQuery(dex.Step[TranslateTextInput]):
    def execute(self, context: dex.Context, input: TranslateTextInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:niutrans group-label:"Niutrans"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[TranslateTextOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[TranslateTextOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TranslateTextFlow(dex.Flow[TranslateTextInput]):
    """Run one Niutrans query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Niutrans Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_translate_text_step(
            step_type="CallProvider",
            step_input_type=TranslateTextInput,
            annotations={
                "groupId": "niutrans",
                "groupLabel": "Niutrans",
                "explanation": "Run Niutrans translateText.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            translated=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "TranslateTextNiutrans"

    def get_steps(self) -> dex.StepList[TranslateTextInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
