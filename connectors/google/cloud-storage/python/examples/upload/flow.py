# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Google Cloud Storage upload Flow using the uploadObject Mutation."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.google_cloud_storage import (
    Connection,
    Object,
    UploadObjectInput,
    new_upload_object_step,
)

FLOW_TYPE = "GoogleCloudStorageUploadPython"
CONNECTION_NAME = "google-cloud-storage"
_CALL_STEP_TYPE = "CallUploadObject"
_RESULT = dex.Attribute("google-cloud-storage-uploaded-python", Object)


def map_to_operation_input(request: UploadObjectInput) -> UploadObjectInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:google-cloud-storage group-label:"Google Cloud Storage"
# dex:explanation text:"Start the Google Cloud Storage request."
class _StartRequest(dex.Step[UploadObjectInput]):
    def get_step_type(self) -> str:
        return "StartUploadObject"

    def execute(self, context: dex.Context, input: UploadObjectInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallUploadObject"), input)


# dex:group group-id:google-cloud-storage group-label:"Google Cloud Storage"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[MutationResult[Object]]):
    def get_step_type(self) -> str:
        return "CompleteUploadObject"

    def execute(self, context: dex.Context, input: MutationResult[Object]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class UploadFlow(dex.Flow[UploadObjectInput]):
    """Run one uploadObject write through the static google-cloud-storage connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the object upload Flow requires the static google-cloud-storage connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_upload_object_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=UploadObjectInput,
            annotations={
                "groupId": "google-cloud-storage",
                "groupLabel": "Google Cloud Storage",
                "explanation": "Upload one object to the selected Cloud Storage bucket.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            uploaded=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "GoogleCloudStorageUploadPython"

    def get_steps(self) -> dex.StepList[UploadObjectInput]:
        """Register the start, provider write, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
