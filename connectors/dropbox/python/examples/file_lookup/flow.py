# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Dropbox file identity through a typed Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.dropbox import Connection, EntryMetadata, GetMetadataInput, new_get_metadata_step

CONNECTION_NAME = "dropbox"
FLOW_TYPE = "DropboxGetMetadataPython"
FILE_RESULT = dex.Attribute("dropbox-python-file-metadata", EntryMetadata)


def map_to_metadata_input(input: GetMetadataInput) -> GetMetadataInput:
    """Pass the validated typed Flow input to the provider Query."""
    return input


# dex:group group-id:dropbox-file group-label:"Dropbox file"
# dex:explanation text:"Save the selected Dropbox file identity."
class CompleteMetadataRead(dex.Step[QueryResult[EntryMetadata]]):
    """Persist the provider result and complete the Flow."""

    def execute(self, context: dex.Context, input: QueryResult[EntryMetadata]) -> dex.StepDecision:
        """Save the returned file identity for external reads."""
        FILE_RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetMetadataFlow(dex.Flow[GetMetadataInput]):
    """Read one file or folder through the static Dropbox connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("getMetadata requires the static Dropbox connection")
        self.complete = CompleteMetadataRead()
        self.read = new_get_metadata_step(
            step_type="ReadDropboxMetadata",
            step_input_type=GetMetadataInput,
            annotations={
                "groupId": "dropbox-file",
                "groupLabel": "Dropbox file",
                "explanation": "Read one Dropbox file or folder identity.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_metadata_input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable example Flow identity."""
        return "DropboxGetMetadataPython"

    def get_steps(self) -> dex.StepList[GetMetadataInput]:
        """Register the provider and completion Steps."""
        return dex.StepList.start_step(self.read).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the retained file Attribute."""
        return dex.PersistenceSchema.of(FILE_RESULT)
