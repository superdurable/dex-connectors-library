# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one OneDrive file's metadata through a typed Dex Flow."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.microsoft_onedrive import Connection, DriveItem, GetFileInput, new_get_file_step

CONNECTION_NAME = "onedrive-files"
FLOW_TYPE = "OneDriveGetFilePython"
FILE_RESULT = dex.Attribute("onedrive-get-file-result", DriveItem)


@dataclass(frozen=True, slots=True)
class GetFileRequest:
    """Static drive and item identity for one metadata read."""

    drive_id: str = field(metadata={"dex_json_name": "driveId"})
    item_id: str = field(metadata={"dex_json_name": "itemId"})

    def to_operation_input(self) -> GetFileInput:
        """Map the Flow input to a Graph item lookup."""
        return GetFileInput(self.drive_id, self.item_id)


# dex:group group-id:onedrive-file group-label:"OneDrive file"
# dex:explanation text:"Store the file metadata returned by Microsoft Graph."
class CompleteFileRead(dex.Step[QueryResult[DriveItem]]):
    """Persist the returned item and complete the Flow."""

    def execute(self, context: dex.Context, input: QueryResult[DriveItem]) -> dex.StepDecision:
        """Save the item for external reads."""
        FILE_RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetFileFlow(dex.Flow[GetFileRequest]):
    """Read one item through the static OneDrive connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("getFile requires the static OneDrive connection")
        self.complete = CompleteFileRead()
        self.read = new_get_file_step(
            step_type="ReadOneDriveFile",
            step_input_type=GetFileRequest,
            annotations={
                "groupId": "onedrive-file",
                "groupLabel": "OneDrive file",
                "explanation": "Read one OneDrive or SharePoint file's metadata.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=GetFileRequest.to_operation_input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[GetFileRequest]:
        """Register the provider Step and completion Step."""
        return dex.StepList.start_step(self.read).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the retained file Attribute."""
        return dex.PersistenceSchema.of(FILE_RESULT)
