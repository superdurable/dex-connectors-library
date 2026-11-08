# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Amplitude user search Flow using searchUsers."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.amplitude import (
    Connection,
    SearchUsersInput,
    UserSearchPage,
    new_search_users_step,
)

FLOW_TYPE = "AmplitudeUserSearchPython"
CONNECTION_NAME = "amplitude"
_CALL_STEP_TYPE = "CallAmplitude"
_RESULT = dex.Attribute("amplitude-user-search", UserSearchPage)


def map_to_operation_input(request: SearchUsersInput) -> SearchUsersInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:amplitude group-label:"Amplitude"
# dex:explanation text:"Start the Amplitude request."
class _StartRequest(dex.Step[SearchUsersInput]):
    def get_step_type(self) -> str:
        return "StartAmplitudeRequest"

    def execute(self, context: dex.Context, input: SearchUsersInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallAmplitude"), input)


# dex:group group-id:amplitude group-label:"Amplitude"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[UserSearchPage]]):
    def get_step_type(self) -> str:
        return "CompleteAmplitudeRequest"

    def execute(self, context: dex.Context, input: QueryResult[UserSearchPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class UserSearchFlow(dex.Flow[SearchUsersInput]):
    """Read user IDs through the static amplitude connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("user search requires the static amplitude connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_search_users_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=SearchUsersInput,
            annotations={
                "groupId": "amplitude",
                "groupLabel": "Amplitude",
                "explanation": "Read user IDs from the connected Amplitude account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            searched=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "AmplitudeUserSearchPython"

    def get_steps(self) -> dex.StepList[SearchUsersInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
