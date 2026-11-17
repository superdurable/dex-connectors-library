# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Bilibili provider result in a durable Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.bilibili import (
    AuthorizedUser,
    Connection,
    GetAuthorizedUserInput,
    new_get_authorized_user_step,
)

CONNECTION_NAME = "bilibili"
_RESULT = dex.Attribute("bilibili-authorized-user", AuthorizedUser)


def map_to_operation_input(input: GetAuthorizedUserInput) -> GetAuthorizedUserInput:
    """Pass the public Flow input to the provider Query."""
    return input


# dex:group group-id:bilibili group-label:"Bilibili"
# dex:explanation text:"Start the Bilibili Query for this Flow."
class _Start(dex.Step[GetAuthorizedUserInput]):
    def get_step_type(self) -> str:
        return "StartAuthorizedUserFlow"

    def execute(self, context: dex.Context, input: GetAuthorizedUserInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("GetAuthorizedUserQuery"), input)


# dex:group group-id:bilibili group-label:"Bilibili"
# dex:explanation text:"Persist the validated provider result and complete the Flow."
class _Complete(dex.Step[QueryResult[AuthorizedUser]]):
    def get_step_type(self) -> str:
        return "CompleteAuthorizedUser"

    def execute(self, context: dex.Context, input: QueryResult[AuthorizedUser]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class AuthorizedUserFlow(dex.Flow[GetAuthorizedUserInput]):
    """Call Bilibili through the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Bilibili connection name differs")
        self._start = _Start()
        self._complete = _Complete()
        self._query = new_get_authorized_user_step(
            step_type="GetAuthorizedUserQuery",
            step_input_type=GetAuthorizedUserInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Bilibili",
                "explanation": "Read the configured Bilibili provider account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable application Flow identity."""
        return "BilibiliAuthorizedUserFlow"

    def get_steps(self) -> dex.StepList[GetAuthorizedUserInput]:
        """Register start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable provider result Attribute."""
        return dex.PersistenceSchema.of(_RESULT)
