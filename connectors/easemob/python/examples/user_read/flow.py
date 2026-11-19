# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Easemob IM user's nonsecret details through a Dex Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.easemob import Connection, GetUserDetailInput, UserDetail, new_get_user_detail_step

CONNECTION_NAME = "easemob"
user_detail_attribute = dex.Attribute("easemob-user-detail-python", UserDetail)


# dex:group group-id:user group-label:"IM user"
# dex:explanation text:"Record the selected account fields and complete the Flow."
class RecordUserDetail(dex.Step[QueryResult[UserDetail]]):
    """Persist selected nonsecret account metadata only."""

    def get_step_type(self) -> str:
        """Return the stable recording Step identity."""
        return "RecordEasemobUserDetail"

    def execute(self, context: dex.Context, input: QueryResult[UserDetail]) -> dex.StepDecision:
        """Record user ID, UUID, timestamps, active state, and nickname."""
        user_detail_attribute.set(context, input.value)
        return dex.graceful_complete(input.value)


class UserReadFlow(dex.Flow[GetUserDetailInput]):
    """Read one user in the connected Easemob application."""

    def __init__(self, connection: Connection) -> None:
        """Bind the provider Query and recording Step."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("user read Flow requires the static Easemob connection")
        self.record = RecordUserDetail()
        self.read = new_get_user_detail_step(
            step_type="GetEasemobUserDetail",
            step_input_type=GetUserDetailInput,
            annotations={
                "groupId": "user",
                "groupLabel": "IM user",
                "explanation": "Acquire an App Token and GET the account detail.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            found=self.record,
        )

    def get_flow_type(self) -> str:
        """Return the durable Flow identity."""
        return "EasemobUserReadPython"

    def get_steps(self) -> dex.StepList[GetUserDetailInput]:
        """Register provider and recording Steps."""
        return dex.StepList.start_step(self.read).other_steps(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the nonsecret user detail Attribute."""
        return dex.PersistenceSchema.of(user_detail_attribute)
