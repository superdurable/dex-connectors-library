# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one WPS provider result in a durable Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.kingsoftoffice import (
    Connection,
    ListFolderFilesInput,
    ListFolderFilesOutput,
    new_list_folder_files_step,
)

CONNECTION_NAME = "kingsoftoffice"
_RESULT = dex.Attribute("wps-folder-files", ListFolderFilesOutput)


def map_to_operation_input(input: ListFolderFilesInput) -> ListFolderFilesInput:
    """Pass the public Flow input to the provider Query."""
    return input


# dex:group group-id:kingsoftoffice group-label:"WPS 365"
# dex:explanation text:"Start the WPS folder metadata Query for this Flow."
class _Start(dex.Step[ListFolderFilesInput]):
    def get_step_type(self) -> str:
        return "StartFolderFilesFlow"

    def execute(self, context: dex.Context, input: ListFolderFilesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListFolderFilesQuery"), input)


# dex:group group-id:kingsoftoffice group-label:"WPS 365"
# dex:explanation text:"Persist the validated folder page and complete the Flow."
class _Complete(dex.Step[QueryResult[ListFolderFilesOutput]]):
    def get_step_type(self) -> str:
        return "CompleteFolderFiles"

    def execute(self, context: dex.Context, input: QueryResult[ListFolderFilesOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class FolderFilesFlow(dex.Flow[ListFolderFilesInput]):
    """Call WPS through the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("WPS connection name differs")
        self._start = _Start()
        self._complete = _Complete()
        self._query = new_list_folder_files_step(
            step_type="ListFolderFilesQuery",
            step_input_type=ListFolderFilesInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "WPS 365",
                "explanation": "Read one WPS folder metadata page.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable application Flow identity."""
        return "WPSFolderFilesFlow"

    def get_steps(self) -> dex.StepList[ListFolderFilesInput]:
        """Register start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable provider result Attribute."""
        return dex.PersistenceSchema.of(_RESULT)
