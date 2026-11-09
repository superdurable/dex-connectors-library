# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Google Cloud Storage download Flow using the downloadObject Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.google_cloud_storage import (
    Connection,
    DownloadedObject,
    DownloadObjectInput,
    new_download_object_step,
)

FLOW_TYPE = "GoogleCloudStorageDownloadPython"
CONNECTION_NAME = "google-cloud-storage"
_CALL_STEP_TYPE = "CallDownloadObject"
_RESULT = dex.Attribute("google-cloud-storage-python-download", DownloadedObject)


def map_to_operation_input(request: DownloadObjectInput) -> DownloadObjectInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:google-cloud-storage group-label:"Google Cloud Storage"
# dex:explanation text:"Start the Google Cloud Storage request."
class _StartRequest(dex.Step[DownloadObjectInput]):
    def get_step_type(self) -> str:
        return "StartDownloadObject"

    def execute(self, context: dex.Context, input: DownloadObjectInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallDownloadObject"), input)


# dex:group group-id:google-cloud-storage group-label:"Google Cloud Storage"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[DownloadedObject]]):
    def get_step_type(self) -> str:
        return "CompleteDownloadObject"

    def execute(self, context: dex.Context, input: QueryResult[DownloadedObject]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class DownloadFlow(dex.Flow[DownloadObjectInput]):
    """Run one downloadObject read through the static google-cloud-storage connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the download lookup Flow requires the static google-cloud-storage connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_download_object_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=DownloadObjectInput,
            annotations={
                "groupId": "google-cloud-storage",
                "groupLabel": "Google Cloud Storage",
                "explanation": "Download the selected object from Google Cloud Storage.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            downloaded=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "GoogleCloudStorageDownloadPython"

    def get_steps(self) -> dex.StepList[DownloadObjectInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
