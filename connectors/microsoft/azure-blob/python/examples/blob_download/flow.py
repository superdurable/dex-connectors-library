# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Azure Blob Storage blob download Flow using getBlob."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.microsoft_azure_blob import (
    BlobDownload,
    Connection,
    GetBlobInput,
    new_get_blob_step,
)

FLOW_TYPE = "MicrosoftAzureBlobDownloadPython"
CONNECTION_NAME = "microsoft-azure-blob"
_CALL_STEP_TYPE = "CallGetBlob"
_RESULT = dex.Attribute("azureblob-blob-download", BlobDownload)


def map_to_operation_input(request: GetBlobInput) -> GetBlobInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:azureblob group-label:"Azure Blob Storage"
# dex:explanation text:"Start the Azure Blob Storage request."
class _StartRequest(dex.Step[GetBlobInput]):
    def get_step_type(self) -> str:
        return "StartGetBlobRequest"

    def execute(self, context: dex.Context, input: GetBlobInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetBlob"), input)


# dex:group group-id:azureblob group-label:"Azure Blob Storage"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[BlobDownload]]):
    def get_step_type(self) -> str:
        return "CompleteGetBlobRequest"

    def execute(self, context: dex.Context, input: QueryResult[BlobDownload]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class BlobDownloadFlow(dex.Flow[GetBlobInput]):
    """Read one Azure blob through the static microsoft-azure-blob connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("blob download requires the static microsoft-azure-blob connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_blob_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetBlobInput,
            annotations={
                "groupId": "azureblob",
                "groupLabel": "Azure Blob Storage",
                "explanation": "Read one Azure blob from the connected Azure Blob Storage account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            downloaded=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "MicrosoftAzureBlobDownloadPython"

    def get_steps(self) -> dex.StepList[GetBlobInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
