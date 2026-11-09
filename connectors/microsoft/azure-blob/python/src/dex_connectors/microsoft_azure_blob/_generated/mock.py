# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import BlobDownload, GetBlobInput
from .definitions import CONNECTOR_ID, DEFECT, DOWNLOADED, GET_BLOB_DEFINITION, INVALID_RESPONSE, MISSING, PROVIDER_REJECTED

def get_blob_downloaded(value: BlobDownload, failure: Failure | None = None) -> Case[BlobDownload]:
    return Case.branch_result(DOWNLOADED, value, failure)

setattr(get_blob_downloaded, '__module__', "dex_connectors.microsoft_azure_blob.azureblobmock")

def get_blob_missing(value: BlobDownload, failure: Failure | None = None) -> Case[BlobDownload]:
    return Case.branch_result(MISSING, value, failure)

setattr(get_blob_missing, '__module__', "dex_connectors.microsoft_azure_blob.azureblobmock")

def get_blob_provider_rejected(value: BlobDownload, failure: Failure | None = None) -> Case[BlobDownload]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_blob_provider_rejected, '__module__', "dex_connectors.microsoft_azure_blob.azureblobmock")

def get_blob_invalid_response(value: BlobDownload, failure: Failure | None = None) -> Case[BlobDownload]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_blob_invalid_response, '__module__', "dex_connectors.microsoft_azure_blob.azureblobmock")

def get_blob_defect(value: BlobDownload, failure: Failure | None = None) -> Case[BlobDownload]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_blob_defect, '__module__', "dex_connectors.microsoft_azure_blob.azureblobmock")

def get_blob_retry(failure: Failure, after_seconds: float = 0) -> Case[BlobDownload]:
    return Case.retry(failure, after_seconds)

setattr(get_blob_retry, '__module__', "dex_connectors.microsoft_azure_blob.azureblobmock")

def get_blob_downloaded_default() -> Case[BlobDownload]:
    return Case.branch_result(DOWNLOADED, BlobDownload(data_base64='aGVsbG8=', content_type='text/plain', etag='"0x1"'))

setattr(get_blob_downloaded_default, '__module__', "dex_connectors.microsoft_azure_blob.azureblobmock")


class Mock:

    def __init__(self, connection_name: str = "microsoft-azure-blob") -> None:
        self.get_blob_script: Query[GetBlobInput, BlobDownload] = Query(GET_BLOB_DEFINITION)
        self.get_blob_script.default(get_blob_downloaded_default())
        self._connection = new_connection_with_operations(
            get_blob=self.get_blob_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.microsoft_azure_blob.azureblobmock")

__all__ = [
    "Mock",
    "get_blob_downloaded",
    "get_blob_missing",
    "get_blob_provider_rejected",
    "get_blob_invalid_response",
    "get_blob_defect",
    "get_blob_retry",
    "get_blob_downloaded_default",
]
