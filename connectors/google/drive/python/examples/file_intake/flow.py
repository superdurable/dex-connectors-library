# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Every file created in the configured Drive folder starts one Flow, which reads its current metadata."""

from dataclasses import dataclass, field, replace

import dex
from dex_connectors.sdk import (
    QueryResult,
    TriggerBindingDefinition,
    TriggerEvent,
)

from dex_connectors.google_drive import (
    Connection,
    File,
    FileEvent,
    GetFileInput,
    define_file_created_trigger_binding,
    new_get_file_step,
)

CONNECTION_NAME = "google-drive-files"
NEW_FILE_TRIGGER_BINDING = "google-drive-file-intake-new-file"
FLOW_TYPE = "GoogleDriveFileIntakePython"


@dataclass(frozen=True, slots=True)
class FileIntakeInput:
    """The Flow start input that the fileCreated Trigger maps from one event."""

    event_id: str = field(metadata={"dex_json_name": "eventId"})
    """The Trigger event ID, such as 1AbC@created."""
    file: FileEvent = field(default_factory=FileEvent)
    """The file metadata the change log reported."""


@dataclass(frozen=True, slots=True)
class FileIntakeOutcome:
    """The durable result of one intake and the Flow's completion output."""

    event_id: str = field(metadata={"dex_json_name": "eventId"})
    """The Trigger event that started the Flow."""
    reported: FileEvent = field(default_factory=FileEvent)
    """The metadata the change log reported."""
    current: File | None = field(default=None, metadata={"dex_json_omit_default": True})
    """The metadata read from Drive when the Flow ran; None until it is read."""


outcome_attribute = dex.Attribute("google-drive-python-file-intake-outcome", FileIntakeOutcome)


def resolve_flow_id(event: TriggerEvent[FileEvent]) -> str:
    """Derive one Flow ID per created file, so a redelivered event starts no second Flow."""
    return f"google-drive-python-file-intake-{CONNECTION_NAME}-{event.payload.file_id}"


def map_to_flow_input(event: TriggerEvent[FileEvent]) -> FileIntakeInput:
    """Map one fileCreated event to the Flow start input."""
    return FileIntakeInput(event_id=event.id, file=event.payload)


def accepts_created_file(event: TriggerEvent[FileEvent]) -> bool:
    """Accept the binding's fileCreated events, which the folder and type filters already matched."""
    return event.id.endswith("@created")


# dex:group group-id:file-intake-receive group-label:"Receive file"
# dex:explanation text:"Record the new file that the fileCreated Trigger reported."
class RecordNewFile(dex.Step[FileIntakeInput]):
    """Persist the reported metadata before the provider read."""

    def execute(self, context: dex.Context, input: FileIntakeInput) -> dex.StepDecision:
        """Record the outcome and read the file's current metadata."""
        outcome_attribute.set(context, FileIntakeOutcome(event_id=input.event_id, reported=input.file))
        return dex.go_to(dex.StepRef("ReadNewFile"), input)


# dex:group group-id:file-intake-complete group-label:"Complete intake"
# dex:explanation text:"Record the file's current metadata and complete the Flow."
class CompleteFileIntake(dex.Step[QueryResult[File]]):
    """Complete with the reported and current metadata."""

    def execute(self, context: dex.Context, input: QueryResult[File]) -> dex.StepDecision:
        """Save the current metadata and return the outcome."""
        outcome = replace(outcome_attribute.get(context), current=input.value)
        outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


class FileIntakeFlow(dex.Flow[FileIntakeInput]):
    """Handle one newly created Drive file."""

    def __init__(self, connection: Connection) -> None:
        """Bind the Google Drive connection used by the metadata read."""
        if connection is None or connection.reference.name != CONNECTION_NAME:
            raise ValueError("Google Drive file intake requires the static connection")
        self.record = RecordNewFile()
        self.complete = CompleteFileIntake()
        self.read = new_get_file_step(
            step_type="ReadNewFile",
            step_input_type=FileIntakeInput,
            annotations={
                "groupId": "google-drive",
                "groupLabel": "Google Drive",
                "explanation": "Read the new file's current metadata from Drive.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda request: GetFileInput(request.file.file_id),
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Keep the application Flow identity stable across Worker versions."""
        return "GoogleDriveFileIntakePython"

    def get_steps(self) -> dex.StepList[FileIntakeInput]:
        """Record, read, and complete."""
        return dex.StepList.start_step(self.record).other_steps(self.read, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the outcome Attribute."""
        return dex.PersistenceSchema.of(outcome_attribute)

    # Dex Web configures this static binding; the analyzer requires a lone return statement.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_file_created_trigger_binding(
                connection_name="google-drive-files",
                binding_name="google-drive-file-intake-new-file",
            )
        ]
