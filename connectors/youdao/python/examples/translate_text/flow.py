# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Youdao translate_text Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.youdao import (
    Connection,
    TextTranslation,
    TranslateTextInput,
    new_translate_text_step,
)

CONNECTION_NAME = "youdao"
_RESULT = dex.Attribute("youdao-result", TextTranslation)


def map_to_operation_input(request: TranslateTextInput) -> TranslateTextInput:
    """Forward the provider request."""
    return request


# dex:group group-id:youdao group-label:"Youdao"
# dex:explanation text:"Start the Youdao query."
class _StartQuery(dex.Step[TranslateTextInput]):
    def execute(self, context: dex.Context, input: TranslateTextInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:youdao group-label:"Youdao"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[TextTranslation]]):
    def execute(self, context: dex.Context, input: QueryResult[TextTranslation]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TranslateTextFlow(dex.Flow[TranslateTextInput]):
    """Run one Youdao query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Youdao Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_translate_text_step(
            step_type="CallProvider",
            step_input_type=TranslateTextInput,
            annotations={
                "groupId": "youdao",
                "groupLabel": "Youdao",
                "explanation": "Run Youdao translate_text.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            translated=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "TranslateTextYoudao"

    def get_steps(self) -> dex.StepList[TranslateTextInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
