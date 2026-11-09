# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Braze profile inspection Flow using getUserProfile."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.braze import (
    Connection,
    GetUserProfileInput,
    UserProfile,
    new_get_user_profile_step,
)

FLOW_TYPE = "BrazeProfileInspectionPython"
CONNECTION_NAME = "braze"
_CALL_STEP_TYPE = "CallBraze"
_RESULT = dex.Attribute("braze-profile", UserProfile)


def map_to_operation_input(request: GetUserProfileInput) -> GetUserProfileInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:braze group-label:"Braze"
# dex:explanation text:"Start the Braze request."
class _StartRequest(dex.Step[GetUserProfileInput]):
    def get_step_type(self) -> str:
        return "StartBrazeRequest"

    def execute(self, context: dex.Context, input: GetUserProfileInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallBraze"), input)


# dex:group group-id:braze group-label:"Braze"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[UserProfile]]):
    def get_step_type(self) -> str:
        return "CompleteBrazeRequest"

    def execute(self, context: dex.Context, input: QueryResult[UserProfile]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ProfileInspectionFlow(dex.Flow[GetUserProfileInput]):
    """Read one profile through the static braze connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("profile inspection requires the static braze connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_user_profile_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetUserProfileInput,
            annotations={
                "groupId": "braze",
                "groupLabel": "Braze",
                "explanation": "Read one profile from the connected Braze account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "BrazeProfileInspectionPython"

    def get_steps(self) -> dex.StepList[GetUserProfileInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
