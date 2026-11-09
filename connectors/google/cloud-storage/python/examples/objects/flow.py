# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Google Cloud Storage page Flow using the listObjects Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.google_cloud_storage import (
    Connection,
    ListObjectsInput,
    ObjectsPage,
    new_list_objects_step,
)

FLOW_TYPE = "GoogleCloudStorageListPython"
CONNECTION_NAME = "google-cloud-storage"
_CALL_STEP_TYPE = "CallListObjects"
_RESULT = dex.Attribute("google-cloud-storage-python-page", ObjectsPage)


def map_to_operation_input(request: ListObjectsInput) -> ListObjectsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:google-cloud-storage group-label:"Google Cloud Storage"
# dex:explanation text:"Start the Google Cloud Storage request."
class _StartRequest(dex.Step[ListObjectsInput]):
    def get_step_type(self) -> str:
        return "StartListObjects"

    def execute(self, context: dex.Context, input: ListObjectsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListObjects"), input)


# dex:group group-id:google-cloud-storage group-label:"Google Cloud Storage"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[ObjectsPage]]):
    def get_step_type(self) -> str:
        return "CompleteListObjects"

    def execute(self, context: dex.Context, input: QueryResult[ObjectsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ObjectsFlow(dex.Flow[ListObjectsInput]):
    """Run one listObjects read through the static google-cloud-storage connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the objects lookup Flow requires the static google-cloud-storage connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_objects_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListObjectsInput,
            annotations={
                "groupId": "google-cloud-storage",
                "groupLabel": "Google Cloud Storage",
                "explanation": "Read one Google Cloud Storage page.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "GoogleCloudStorageListPython"

    def get_steps(self) -> dex.StepList[ListObjectsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
