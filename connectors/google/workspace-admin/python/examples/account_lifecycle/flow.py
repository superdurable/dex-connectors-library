# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""List one Directory page through a static Google Workspace connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.google_workspace_admin import (
    Connection,
    ListUsersInput,
    ListUsersOutput,
    new_list_users_step,
)

CONNECTION_NAME = "google-workspace-admin"
_PAGE = dex.Attribute("google-workspace-admin-users-page", ListUsersOutput)


# dex:group group-id:workspace-admin group-label:"Workspace Admin"
# dex:explanation text:"Persist the account page returned by Google and complete the Flow."
class CompleteUserList(dex.Step[QueryResult[ListUsersOutput]]):
    """Complete the Flow with non-secret account metadata."""

    def execute(self, context: dex.Context, input: QueryResult[ListUsersOutput]) -> dex.StepDecision:
        """Store the page in the Flow schema before completing."""
        _PAGE.set(context, input.value)
        return dex.graceful_complete(input.value)


class AccountListFlow(dex.Flow[ListUsersInput]):
    """Request one bounded page of Workspace users."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("account list requires the static Google Workspace connection")
        self.complete = CompleteUserList()
        self.list_users = new_list_users_step(
            step_type="ListWorkspaceUsers",
            step_input_type=ListUsersInput,
            annotations={
                "groupId": "workspace-admin",
                "groupLabel": "Workspace Admin",
                "explanation": "List one bounded page of Google Workspace accounts.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            listed=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable example Flow identity."""
        return "GoogleWorkspaceAdminAccountListPython"

    def get_steps(self) -> dex.StepList[ListUsersInput]:
        """Register the provider Query and completion Step."""
        return dex.StepList.start_step(self.list_users).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the page Attribute consumed by Dex."""
        return dex.PersistenceSchema.of(_PAGE)
