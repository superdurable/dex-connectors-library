# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable RongCloud checkUserOnline Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.rongcloud import (
    CheckUserOnlineInput,
    CheckUserOnlineOutput,
    Connection,
    new_check_user_online_step,
)

CONNECTION_NAME = "rongcloud"
_RESULT = dex.Attribute("rongcloud-result", CheckUserOnlineOutput)


def map_to_operation_input(request: CheckUserOnlineInput) -> CheckUserOnlineInput:
    """Forward the provider request."""
    return request


# dex:group group-id:rongcloud group-label:"RongCloud"
# dex:explanation text:"Start the RongCloud query."
class _StartQuery(dex.Step[CheckUserOnlineInput]):
    def execute(self, context: dex.Context, input: CheckUserOnlineInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:rongcloud group-label:"RongCloud"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[CheckUserOnlineOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[CheckUserOnlineOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class CheckUserOnlineFlow(dex.Flow[CheckUserOnlineInput]):
    """Run one RongCloud query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("RongCloud Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_check_user_online_step(
            step_type="CallProvider",
            step_input_type=CheckUserOnlineInput,
            annotations={
                "groupId": "rongcloud",
                "groupLabel": "RongCloud",
                "explanation": "Run RongCloud checkUserOnline.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            checked=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "CheckUserOnlineRongCloud"

    def get_steps(self) -> dex.StepList[CheckUserOnlineInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
