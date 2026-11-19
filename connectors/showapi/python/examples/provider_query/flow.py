# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Showapi provider data in a durable Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.showapi import (
    AreaCodeMatches,
    Connection,
    LookupAreaCodesInput,
    new_lookup_area_codes_step,
)

CONNECTION_NAME = "showapi"
_RESULT = dex.Attribute("showapi-query-result", AreaCodeMatches)


def map_to_operation_input(input: LookupAreaCodesInput) -> LookupAreaCodesInput:
    """Pass the application Query input to the provider."""
    return input


# dex:group group-id:showapi group-label:"Showapi"
# dex:explanation text:"Start the Showapi Query."
class _StartQuery(dex.Step[LookupAreaCodesInput]):
    def get_step_type(self) -> str:
        return "StartShowapiQuery"

    def execute(self, context: dex.Context, input: LookupAreaCodesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallShowapi"), input)


# dex:group group-id:showapi group-label:"Showapi"
# dex:explanation text:"Persist the Showapi Query result and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[AreaCodeMatches]]):
    def get_step_type(self) -> str:
        return "CompleteShowapiQuery"

    def execute(self, context: dex.Context, input: QueryResult[AreaCodeMatches]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ProviderQueryFlow(dex.Flow[LookupAreaCodesInput]):
    """Read the configured Showapi account through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ProviderQueryFlow requires the static Showapi connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_lookup_area_codes_step(
            step_type="CallShowapi",
            step_input_type=LookupAreaCodesInput,
            annotations={
                "groupId": "showapi",
                "groupLabel": "Showapi",
                "explanation": "Read Showapi account data.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Showapi Query Flow identity."""
        return "ShowapiProviderQuery"

    def get_steps(self) -> dex.StepList[LookupAreaCodesInput]:
        """Register start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider Query output."""
        return dex.PersistenceSchema.of(_RESULT)
