# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Qingflow application names in a durable Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.qingflow import (
    Connection,
    ListApplicationsInput,
    ListApplicationsOutput,
    new_list_applications_step,
)

CONNECTION_NAME = "qingflow"
_APPLICATIONS = dex.Attribute("qingflow-application-directory", ListApplicationsOutput)


def map_to_operation_input(input: ListApplicationsInput) -> ListApplicationsInput:
    """Pass the empty application-list input to Qingflow."""
    return input


# dex:group group-id:qingflow group-label:"Qingflow"
# dex:explanation text:"Start the Qingflow application directory request."
class _StartApplications(dex.Step[ListApplicationsInput]):
    def get_step_type(self) -> str:
        return "StartQingflowApplications"

    def execute(self, context: dex.Context, input: ListApplicationsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallQingflow"), input)


# dex:group group-id:qingflow group-label:"Qingflow"
# dex:explanation text:"Persist the Qingflow application directory and complete the Flow."
class _CompleteApplications(dex.Step[QueryResult[ListApplicationsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteQingflowApplications"

    def execute(self, context: dex.Context, input: QueryResult[ListApplicationsOutput]) -> dex.StepDecision:
        _APPLICATIONS.set(context, input.value)
        return dex.graceful_complete(input.value)


class ApplicationDirectoryFlow(dex.Flow[ListApplicationsInput]):
    """List application keys through the static Qingflow connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ApplicationDirectoryFlow requires the static Qingflow connection")
        self._start = _StartApplications()
        self._complete = _CompleteApplications()
        self._call = new_list_applications_step(
            step_type="CallQingflow",
            step_input_type=ListApplicationsInput,
            annotations={
                "groupId": "qingflow",
                "groupLabel": "Qingflow",
                "explanation": "List workspace applications.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable application directory Flow identity."""
        return "QingflowApplicationDirectory"

    def get_steps(self) -> dex.StepList[ListApplicationsInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the application directory output."""
        return dex.PersistenceSchema.of(_APPLICATIONS)
