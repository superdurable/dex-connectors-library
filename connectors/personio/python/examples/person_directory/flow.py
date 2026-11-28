# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Personio person directory Flow using listPersons."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.personio import (
    Connection,
    ListPersonsInput,
    PersonPage,
    new_list_persons_step,
)

FLOW_TYPE = "PersonioPersonDirectoryPython"
CONNECTION_NAME = "personio"
_CALL_STEP_TYPE = "ListPersonioPersons"
_RESULT = dex.Attribute("personio-person-page", PersonPage)


def map_to_operation_input(request: ListPersonsInput) -> ListPersonsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:personio group-label:"Personio"
# dex:explanation text:"Start the Personio request."
class _StartRequest(dex.Step[ListPersonsInput]):
    def get_step_type(self) -> str:
        return "StartPersonioDirectory"

    def execute(self, context: dex.Context, input: ListPersonsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListPersonioPersons"), input)


# dex:group group-id:personio group-label:"Personio"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[PersonPage]]):
    def get_step_type(self) -> str:
        return "CompletePersonioDirectoryRequest"

    def execute(self, context: dex.Context, input: QueryResult[PersonPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class PersonDirectoryFlow(dex.Flow[ListPersonsInput]):
    """Read one page through the static personio connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("person directory requires the static personio connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_persons_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListPersonsInput,
            annotations={
                "groupId": "personio",
                "groupLabel": "Personio",
                "explanation": "Read one page from the connected Personio account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "PersonioPersonDirectoryPython"

    def get_steps(self) -> dex.StepList[ListPersonsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
