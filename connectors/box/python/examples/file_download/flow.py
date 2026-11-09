# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Download one Box file through a typed Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.box import Connection, DownloadedFile, DownloadFileInput, new_download_file_step

CONNECTION_NAME = "box"
FILE_RESULT = dex.Attribute("box-python-downloaded-file", DownloadedFile)


def map_to_download_input(input: DownloadFileInput) -> DownloadFileInput:
    """Pass the typed Flow input to the provider Query."""
    return input


# dex:group group-id:box-file group-label:"Box file"
# dex:explanation text:"Save the downloaded Box file content."
class CompleteDownload(dex.Step[QueryResult[DownloadedFile]]):
    """Persist the bounded provider result and complete the Flow."""

    def execute(self, context: dex.Context, input: QueryResult[DownloadedFile]) -> dex.StepDecision:
        """Save the returned file content for external reads."""
        FILE_RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class DownloadFileFlow(dex.Flow[DownloadFileInput]):
    """Read one file through the static Box connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("downloadFile requires the static Box connection")
        self.complete = CompleteDownload()
        self.download = new_download_file_step(
            step_type="DownloadBoxFile",
            step_input_type=DownloadFileInput,
            annotations={
                "groupId": "box-file",
                "groupLabel": "Box file",
                "explanation": "Download bounded content from one Box file.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_download_input,
            downloaded=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable example Flow identity."""
        return "BoxDownloadFilePython"

    def get_steps(self) -> dex.StepList[DownloadFileInput]:
        """Register the provider and completion Steps."""
        return dex.StepList.start_step(self.download).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the retained file Attribute."""
        return dex.PersistenceSchema.of(FILE_RESULT)
