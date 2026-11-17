# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one note task through a statically named Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.dedao import (
    Connection,
    GetNoteTaskStatusInput,
    GetNoteTaskStatusOutput,
    new_get_note_task_status_step,
)

CONNECTION_NAME = "dedao"
_OUTCOME = dex.Attribute("dedao-get-note-task-status-outcome", GetNoteTaskStatusOutput)


def map_to_operation_input(input: GetNoteTaskStatusInput) -> GetNoteTaskStatusInput:
    """Pass the existing task ID to the provider Query."""
    return input


# dex:group group-id:get-note-task-status group-label:"Get note task status"
# dex:explanation text:"Start the Dedao note task read."
class _StartRequest(dex.Step[GetNoteTaskStatusInput]):
    def get_step_type(self) -> str:
        return "StartGetNoteTaskStatus"

    def execute(self, context: dex.Context, input: GetNoteTaskStatusInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetNoteTaskStatus"), input)


# dex:group group-id:get-note-task-status group-label:"Get note task status"
# dex:explanation text:"Persist task status, then complete the Flow."
class _CompleteResult(dex.Step[QueryResult[GetNoteTaskStatusOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetNoteTaskStatus"

    def execute(self, context: dex.Context, input: QueryResult[GetNoteTaskStatusOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetNoteTaskStatusFlow(dex.Flow[GetNoteTaskStatusInput]):
    """Read a note task from the static `dedao` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("task status query requires the static dedao connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._read = new_get_note_task_status_step(
            step_type="CallGetNoteTaskStatus",
            step_input_type=GetNoteTaskStatusInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Dedao",
                "explanation": "Call Dedao's getNoteTaskStatus operation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable type shown in Dex Web."""
        return "DedaoGetNoteTaskStatus"

    def get_steps(self) -> dex.StepList[GetNoteTaskStatusInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the returned task status and optional note ID."""
        return dex.PersistenceSchema.of(_OUTCOME)
