# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read kingsoftcloud data through the static Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.kingsoftcloud import Connection, ListUsersInput, ListUsersOutput, new_list_users_step

CONNECTION_NAME = "kingsoftcloud"
_OUTCOME = dex.Attribute("kingsoftcloud-list-users-result", ListUsersOutput)


def map_to_operation_input(input: ListUsersInput) -> ListUsersInput:
    """Pass the typed request to the provider Query."""
    return input


# dex:group group-id:list-users group-label:"ListUsers"
# dex:explanation text:"Start the kingsoftcloud Query."
class _StartRequest(dex.Step[ListUsersInput]):
    def get_step_type(self) -> str:
        return "StartListUsers"

    def execute(self, context: dex.Context, input: ListUsersInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListUsers"), input)


# dex:group group-id:list-users group-label:"ListUsers"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[ListUsersOutput]]):
    def get_step_type(self) -> str:
        return "CompleteListUsers"

    def execute(self, context: dex.Context, input: QueryResult[ListUsersOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListUsersFlow(dex.Flow[ListUsersInput]):
    """Run listUsers using the static `kingsoftcloud` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("kingsoftcloud example requires the static connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._query = new_list_users_step(
            step_type="CallListUsers",
            step_input_type=ListUsersInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "kingsoftcloud",
                "explanation": "Call the kingsoftcloud listUsers Query.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "KingsoftcloudListUsers"

    def get_steps(self) -> dex.StepList[ListUsersInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the typed provider output."""
        return dex.PersistenceSchema.of(_OUTCOME)
