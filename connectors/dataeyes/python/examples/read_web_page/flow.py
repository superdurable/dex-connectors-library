# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one public page through a statically named Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.dataeyes import Connection, ReadWebPageInput, ReadWebPageOutput, new_read_web_page_step

CONNECTION_NAME = "dataeyes"
_OUTCOME = dex.Attribute("dataeyes-read-web-page-outcome", ReadWebPageOutput)


def map_to_operation_input(input: ReadWebPageInput) -> ReadWebPageInput:
    """Pass the public URL to the provider Query."""
    return input


# dex:group group-id:read-web-page group-label:"Read web page"
# dex:explanation text:"Start the DataEyes public page read."
class _StartRequest(dex.Step[ReadWebPageInput]):
    def get_step_type(self) -> str:
        return "StartReadWebPage"

    def execute(self, context: dex.Context, input: ReadWebPageInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallReadWebPage"), input)


# dex:group group-id:read-web-page group-label:"Read web page"
# dex:explanation text:"Persist title and Markdown, then complete the Flow."
class _CompleteResult(dex.Step[QueryResult[ReadWebPageOutput]]):
    def get_step_type(self) -> str:
        return "CompleteReadWebPage"

    def execute(self, context: dex.Context, input: QueryResult[ReadWebPageOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ReadWebPageFlow(dex.Flow[ReadWebPageInput]):
    """Read a public page from the static `dataeyes` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("page read requires the static dataeyes connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._read = new_read_web_page_step(
            step_type="CallReadWebPage",
            step_input_type=ReadWebPageInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "DataEyes",
                "explanation": "Call DataEyes's readWebPage operation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable type shown in Dex Web."""
        return "DataEyesReadWebPage"

    def get_steps(self) -> dex.StepList[ReadWebPageInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the page title and Markdown result."""
        return dex.PersistenceSchema.of(_OUTCOME)
