# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read pgyer through one durable provider Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.pgyer import (
    Connection,
    ListUploadedAppsInput,
    UploadedAppPage,
    new_list_uploaded_apps_step,
)

CONNECTION_NAME = "pgyer"
_OUTCOME = dex.Attribute("pgyer-list_uploaded_apps-outcome", UploadedAppPage)


def map_to_operation_input(input: ListUploadedAppsInput) -> ListUploadedAppsInput:
    """Forward the typed Flow input to the provider Query."""
    return input


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Begin the pgyer read with the configured connection."
class _Start(dex.Step[ListUploadedAppsInput]):
    def get_step_type(self) -> str:
        return "StartListUploadedApps"

    def execute(self, context: dex.Context, input: ListUploadedAppsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListUploadedAppsQuery"), input)


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _Complete(dex.Step[QueryResult[UploadedAppPage]]):
    def get_step_type(self) -> str:
        return "CompleteListUploadedApps"

    def execute(self, context: dex.Context, input: QueryResult[UploadedAppPage]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListUploadedAppsFlow(dex.Flow[ListUploadedAppsInput]):
    """Durably read pgyer with its static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("pgyer Flow requires its static connection")
        self._start = _Start()
        self._complete = _Complete()
        self._query = new_list_uploaded_apps_step(
            step_type="ListUploadedAppsQuery",
            step_input_type=ListUploadedAppsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "pgyer",
                "explanation": "Call pgyer listUploadedApps.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity shown in Dex Web."""
        return "PgyerListUploadedApps"

    def get_steps(self) -> dex.StepList[ListUploadedAppsInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable provider result Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
