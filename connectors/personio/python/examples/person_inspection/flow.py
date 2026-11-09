# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Personio person inspection Flow using getPerson."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.personio import (
    Connection,
    GetPersonInput,
    PersonProfile,
    new_get_person_step,
)

FLOW_TYPE = "PersonioPersonInspectionPython"
CONNECTION_NAME = "personio"
_CALL_STEP_TYPE = "CallPersonio"
_RESULT = dex.Attribute("personio-person", PersonProfile)


def map_to_operation_input(request: GetPersonInput) -> GetPersonInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:personio group-label:"Personio"
# dex:explanation text:"Start the Personio request."
class _StartRequest(dex.Step[GetPersonInput]):
    def get_step_type(self) -> str:
        return "StartPersonioRequest"

    def execute(self, context: dex.Context, input: GetPersonInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallPersonio"), input)


# dex:group group-id:personio group-label:"Personio"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[PersonProfile]]):
    def get_step_type(self) -> str:
        return "CompletePersonioRequest"

    def execute(self, context: dex.Context, input: QueryResult[PersonProfile]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class PersonInspectionFlow(dex.Flow[GetPersonInput]):
    """Read one person through the static personio connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("person inspection requires the static personio connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_person_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetPersonInput,
            annotations={
                "groupId": "personio",
                "groupLabel": "Personio",
                "explanation": "Read one person from the connected Personio account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "PersonioPersonInspectionPython"

    def get_steps(self) -> dex.StepList[GetPersonInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
