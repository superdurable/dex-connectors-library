# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable NetEase Yunxin user-profile Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.netease import (
    Connection,
    GetUserProfileInput,
    GetUserProfileOutput,
    new_get_user_profile_step,
)

CONNECTION_NAME = "netease"
_RESULT = dex.Attribute("netease-user-profile", GetUserProfileOutput)


def map_to_operation_input(request: GetUserProfileInput) -> GetUserProfileInput:
    """Forward the IM account ID to Yunxin."""
    return request


# dex:group group-id:netease group-label:"NetEase Yunxin"
# dex:explanation text:"Start the NetEase Yunxin user-profile request."
class _StartProfile(dex.Step[GetUserProfileInput]):
    def get_step_type(self) -> str:
        return "StartNetEaseUserProfile"

    def execute(self, context: dex.Context, input: GetUserProfileInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallNetEase"), input)


# dex:group group-id:netease group-label:"NetEase Yunxin"
# dex:explanation text:"Persist current profile and complete the Flow."
class _CompleteProfile(dex.Step[QueryResult[GetUserProfileOutput]]):
    def get_step_type(self) -> str:
        return "CompleteNetEaseUserProfile"

    def execute(self, context: dex.Context, input: QueryResult[GetUserProfileOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class UserProfileFlow(dex.Flow[GetUserProfileInput]):
    """Read one IM profile through the static Yunxin connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("UserProfileFlow requires the static NetEase Yunxin connection")
        self._start = _StartProfile()
        self._complete = _CompleteProfile()
        self._call = new_get_user_profile_step(
            step_type="CallNetEase",
            step_input_type=GetUserProfileInput,
            annotations={
                "groupId": "netease",
                "groupLabel": "NetEase Yunxin",
                "explanation": "Read one IM account profile.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            fetched=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable user-profile Flow identity."""
        return "NetEaseUserProfile"

    def get_steps(self) -> dex.StepList[GetUserProfileInput]:
        """Register the profile Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the user-profile result."""
        return dex.PersistenceSchema.of(_RESULT)
