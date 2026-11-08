# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Iterable user inspection Flow using getUser."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.iterable import (
    Connection,
    GetUserInput,
    UserIdentity,
    new_get_user_step,
)

FLOW_TYPE = "IterableUserInspectionPython"
CONNECTION_NAME = "iterable"
_CALL_STEP_TYPE = "CallIterable"
_RESULT = dex.Attribute("iterable-user", UserIdentity)


def map_to_operation_input(request: GetUserInput) -> GetUserInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:iterable group-label:"Iterable"
# dex:explanation text:"Start the Iterable request."
class _StartRequest(dex.Step[GetUserInput]):
    def get_step_type(self) -> str:
        return "StartIterableRequest"

    def execute(self, context: dex.Context, input: GetUserInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallIterable"), input)


# dex:group group-id:iterable group-label:"Iterable"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[UserIdentity]]):
    def get_step_type(self) -> str:
        return "CompleteIterableRequest"

    def execute(self, context: dex.Context, input: QueryResult[UserIdentity]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class UserInspectionFlow(dex.Flow[GetUserInput]):
    """Read one user through the static iterable connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("user inspection requires the static iterable connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_user_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetUserInput,
            annotations={
                "groupId": "iterable",
                "groupLabel": "Iterable",
                "explanation": "Read one user from the connected Iterable account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "IterableUserInspectionPython"

    def get_steps(self) -> dex.StepList[GetUserInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
