# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import DownloadFileInput, DownloadedFile, UploadFileInput, UploadedFile
from .definitions import CONNECTOR_ID, DEFECT, DOWNLOADED, DOWNLOAD_FILE_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, TOO_LARGE, UPLOADED, UPLOAD_FILE_DEFINITION

def download_file_downloaded(value: DownloadedFile, failure: Failure | None = None) -> Case[DownloadedFile]:
    return Case.branch_result(DOWNLOADED, value, failure)

setattr(download_file_downloaded, '__module__', "dex_connectors.box.boxmock")

def download_file_not_found(value: DownloadedFile, failure: Failure | None = None) -> Case[DownloadedFile]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(download_file_not_found, '__module__', "dex_connectors.box.boxmock")

def download_file_too_large(value: DownloadedFile, failure: Failure | None = None) -> Case[DownloadedFile]:
    return Case.branch_result(TOO_LARGE, value, failure)

setattr(download_file_too_large, '__module__', "dex_connectors.box.boxmock")

def download_file_provider_rejected(value: DownloadedFile, failure: Failure | None = None) -> Case[DownloadedFile]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(download_file_provider_rejected, '__module__', "dex_connectors.box.boxmock")

def download_file_invalid_response(value: DownloadedFile, failure: Failure | None = None) -> Case[DownloadedFile]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(download_file_invalid_response, '__module__', "dex_connectors.box.boxmock")

def download_file_defect(value: DownloadedFile, failure: Failure | None = None) -> Case[DownloadedFile]:
    return Case.branch_result(DEFECT, value, failure)

setattr(download_file_defect, '__module__', "dex_connectors.box.boxmock")

def download_file_retry(failure: Failure, after_seconds: float = 0) -> Case[DownloadedFile]:
    return Case.retry(failure, after_seconds)

setattr(download_file_retry, '__module__', "dex_connectors.box.boxmock")

def download_file_downloaded_default() -> Case[DownloadedFile]:
    return Case.branch_result(DOWNLOADED, DownloadedFile(file_id='12345', content_base64='SGVsbG8=', content_bytes=5))

setattr(download_file_downloaded_default, '__module__', "dex_connectors.box.boxmock")

def upload_file_uploaded(value: UploadedFile, failure: Failure | None = None) -> Case[UploadedFile]:
    return Case.branch_result(UPLOADED, value, failure)

setattr(upload_file_uploaded, '__module__', "dex_connectors.box.boxmock")

def upload_file_not_found(value: UploadedFile, failure: Failure | None = None) -> Case[UploadedFile]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(upload_file_not_found, '__module__', "dex_connectors.box.boxmock")

def upload_file_provider_rejected(value: UploadedFile, failure: Failure | None = None) -> Case[UploadedFile]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(upload_file_provider_rejected, '__module__', "dex_connectors.box.boxmock")

def upload_file_invalid_response(value: UploadedFile, failure: Failure | None = None) -> Case[UploadedFile]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(upload_file_invalid_response, '__module__', "dex_connectors.box.boxmock")

def upload_file_defect(value: UploadedFile, failure: Failure | None = None) -> Case[UploadedFile]:
    return Case.branch_result(DEFECT, value, failure)

setattr(upload_file_defect, '__module__', "dex_connectors.box.boxmock")

def upload_file_retry(failure: Failure, after_seconds: float = 0) -> Case[UploadedFile]:
    return Case.retry(failure, after_seconds)

setattr(upload_file_retry, '__module__', "dex_connectors.box.boxmock")

def upload_file_uploaded_default() -> Case[UploadedFile]:
    return Case.branch_result(UPLOADED, UploadedFile(file_id='12345', name='example.txt', sha1='F7FF9E8B7BB2E09B70935A5D785E0CC5D9D0ABF0'))

setattr(upload_file_uploaded_default, '__module__', "dex_connectors.box.boxmock")


class Mock:

    def __init__(self, connection_name: str = "box") -> None:
        self.download_file_script: Query[DownloadFileInput, DownloadedFile] = Query(DOWNLOAD_FILE_DEFINITION)
        self.download_file_script.default(download_file_downloaded_default())
        self.upload_file_script: Mutation[UploadFileInput, UploadedFile] = Mutation(UPLOAD_FILE_DEFINITION)
        self.upload_file_script.default(upload_file_uploaded_default())
        self._connection = new_connection_with_operations(
            download_file=self.download_file_script,
            upload_file=self.upload_file_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.box.boxmock")

__all__ = [
    "Mock",
    "download_file_downloaded",
    "download_file_not_found",
    "download_file_too_large",
    "download_file_provider_rejected",
    "download_file_invalid_response",
    "download_file_defect",
    "download_file_retry",
    "download_file_downloaded_default",
    "upload_file_uploaded",
    "upload_file_not_found",
    "upload_file_provider_rejected",
    "upload_file_invalid_response",
    "upload_file_defect",
    "upload_file_retry",
    "upload_file_uploaded_default",
]
