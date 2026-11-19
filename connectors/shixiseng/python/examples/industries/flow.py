# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Shixiseng industry-list Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.shixiseng import (
    Connection,
    ListIndustriesInput,
    ListIndustriesOutput,
    new_list_industries_step,
)

CONNECTION_NAME = "shixiseng"
_RESULT = dex.Attribute("shixiseng-industry-list", ListIndustriesOutput)


def map_to_operation_input(request: ListIndustriesInput) -> ListIndustriesInput:
    """Forward the public industry ID to Shixiseng."""
    return request


# dex:group group-id:shixiseng group-label:"Shixiseng"
# dex:explanation text:"Start the public industry-list request."
class _StartIndustries(dex.Step[ListIndustriesInput]):
    def get_step_type(self) -> str:
        return "StartShixisengListIndustries"

    def execute(self, context: dex.Context, input: ListIndustriesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallShixiseng"), input)


# dex:group group-id:shixiseng group-label:"Shixiseng"
# dex:explanation text:"Persist the public industry labels and complete the Flow."
class _CompleteIndustries(dex.Step[QueryResult[ListIndustriesOutput]]):
    def get_step_type(self) -> str:
        return "CompleteShixisengListIndustries"

    def execute(self, context: dex.Context, input: QueryResult[ListIndustriesOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListIndustriesFlow(dex.Flow[ListIndustriesInput]):
    """Read one IM industry labels through the static Shixiseng connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ListIndustriesFlow requires the static Shixiseng connection")
        self._start = _StartIndustries()
        self._complete = _CompleteIndustries()
        self._call = new_list_industries_step(
            step_type="CallShixiseng",
            step_input_type=ListIndustriesInput,
            annotations={
                "groupId": "shixiseng",
                "groupLabel": "Shixiseng",
                "explanation": "Read one public industry industry labels.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable industry-list Flow identity."""
        return "ShixisengIndustries"

    def get_steps(self) -> dex.StepList[ListIndustriesInput]:
        """Register the industry labels Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the industry-list result."""
        return dex.PersistenceSchema.of(_RESULT)
