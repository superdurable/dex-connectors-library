# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Microsoft Entra ID account through a named Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.microsoft_entra_id import Connection, GetUserInput, User, new_get_user_step

CONNECTION_NAME = "entra-directory"
FLOW_TYPE = "EntraIDUserLookup"


# dex:group group-id:entra-id group-label:"Microsoft Entra ID"
# dex:explanation text:"Complete after Microsoft Graph returns the requested account."
class UserFound(dex.Step[QueryResult[User]]):
    """Complete with the bounded account record."""

    def get_step_type(self) -> str:
        """Return the stable completion Step identity."""
        return "EntraIDUserFound"

    def execute(self, context: dex.Context, input: QueryResult[User]) -> dex.StepDecision:
        """Return the account without connection credentials."""
        return dex.graceful_complete(input.value)


class UserLookupFlow(dex.Flow[GetUserInput]):
    """Exercise the operation-specific getUser factory and static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Microsoft Entra ID example requires its static connection name")
        self.found = UserFound()
        self.lookup = new_get_user_step(
            step_type="GetEntraIDUser",
            step_input_type=GetUserInput,
            annotations={
                "groupId": "entra-id",
                "groupLabel": "Microsoft Entra ID",
                "explanation": "Read one account by object ID or user principal name.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            found=self.found,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow type for the Dex Worker."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[GetUserInput]:
        """Register the Graph Query and success target."""
        return dex.StepList.start_step(self.lookup).other_steps(self.found)
