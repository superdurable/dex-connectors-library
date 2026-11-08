# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ContinueFolderInput, DownloadFileInput, DownloadedFile, EntryMetadata, FolderPage, GetMetadataInput, ListFolderInput, UploadFileInput, UploadedFile
from .definitions import CONNECTOR_ID, CONTINUE_FOLDER_DEFINITION, CURSOR_EXPIRED, DEFECT, DOWNLOADED, DOWNLOAD_FILE_DEFINITION, FOUND, GET_METADATA_DEFINITION, INVALID_RESPONSE, LISTED, LIST_FOLDER_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, TOO_LARGE, UPLOADED, UPLOAD_FILE_DEFINITION

def list_folder_listed(value: FolderPage, failure: Failure | None = None) -> Case[FolderPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_folder_listed, '__module__', "dex_connectors.dropbox.dropboxmock")

def list_folder_not_found(value: FolderPage, failure: Failure | None = None) -> Case[FolderPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_folder_not_found, '__module__', "dex_connectors.dropbox.dropboxmock")

def list_folder_provider_rejected(value: FolderPage, failure: Failure | None = None) -> Case[FolderPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_folder_provider_rejected, '__module__', "dex_connectors.dropbox.dropboxmock")

def list_folder_invalid_response(value: FolderPage, failure: Failure | None = None) -> Case[FolderPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_folder_invalid_response, '__module__', "dex_connectors.dropbox.dropboxmock")

def list_folder_defect(value: FolderPage, failure: Failure | None = None) -> Case[FolderPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_folder_defect, '__module__', "dex_connectors.dropbox.dropboxmock")

def list_folder_retry(failure: Failure, after_seconds: float = 0) -> Case[FolderPage]:
    return Case.retry(failure, after_seconds)

setattr(list_folder_retry, '__module__', "dex_connectors.dropbox.dropboxmock")

def list_folder_listed_default() -> Case[FolderPage]:
    return Case.branch_result(LISTED, FolderPage(entries=(EntryMetadata(id='id:example', name='example.txt', path_display='/example.txt', kind='file'),), cursor='example-cursor', has_more=False))

setattr(list_folder_listed_default, '__module__', "dex_connectors.dropbox.dropboxmock")

def get_metadata_found(value: EntryMetadata, failure: Failure | None = None) -> Case[EntryMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_metadata_found, '__module__', "dex_connectors.dropbox.dropboxmock")

def get_metadata_not_found(value: EntryMetadata, failure: Failure | None = None) -> Case[EntryMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_metadata_not_found, '__module__', "dex_connectors.dropbox.dropboxmock")

def get_metadata_provider_rejected(value: EntryMetadata, failure: Failure | None = None) -> Case[EntryMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_metadata_provider_rejected, '__module__', "dex_connectors.dropbox.dropboxmock")

def get_metadata_invalid_response(value: EntryMetadata, failure: Failure | None = None) -> Case[EntryMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_metadata_invalid_response, '__module__', "dex_connectors.dropbox.dropboxmock")

def get_metadata_defect(value: EntryMetadata, failure: Failure | None = None) -> Case[EntryMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_metadata_defect, '__module__', "dex_connectors.dropbox.dropboxmock")

def get_metadata_retry(failure: Failure, after_seconds: float = 0) -> Case[EntryMetadata]:
    return Case.retry(failure, after_seconds)

setattr(get_metadata_retry, '__module__', "dex_connectors.dropbox.dropboxmock")

def get_metadata_found_default() -> Case[EntryMetadata]:
    return Case.branch_result(FOUND, EntryMetadata(id='id:example', name='example.txt', path_display='/example.txt', kind='file'))

setattr(get_metadata_found_default, '__module__', "dex_connectors.dropbox.dropboxmock")

def continue_folder_listed(value: FolderPage, failure: Failure | None = None) -> Case[FolderPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(continue_folder_listed, '__module__', "dex_connectors.dropbox.dropboxmock")

def continue_folder_cursor_expired(value: FolderPage, failure: Failure | None = None) -> Case[FolderPage]:
    return Case.branch_result(CURSOR_EXPIRED, value, failure)

setattr(continue_folder_cursor_expired, '__module__', "dex_connectors.dropbox.dropboxmock")

def continue_folder_provider_rejected(value: FolderPage, failure: Failure | None = None) -> Case[FolderPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(continue_folder_provider_rejected, '__module__', "dex_connectors.dropbox.dropboxmock")

def continue_folder_invalid_response(value: FolderPage, failure: Failure | None = None) -> Case[FolderPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(continue_folder_invalid_response, '__module__', "dex_connectors.dropbox.dropboxmock")

def continue_folder_defect(value: FolderPage, failure: Failure | None = None) -> Case[FolderPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(continue_folder_defect, '__module__', "dex_connectors.dropbox.dropboxmock")

def continue_folder_retry(failure: Failure, after_seconds: float = 0) -> Case[FolderPage]:
    return Case.retry(failure, after_seconds)

setattr(continue_folder_retry, '__module__', "dex_connectors.dropbox.dropboxmock")

def continue_folder_listed_default() -> Case[FolderPage]:
    return Case.branch_result(LISTED, FolderPage(entries=(EntryMetadata(id='id:continued', name='continued.txt', path_display='/continued.txt', kind='file'),), cursor='example-cursor-2', has_more=False))

setattr(continue_folder_listed_default, '__module__', "dex_connectors.dropbox.dropboxmock")

def download_file_downloaded(value: DownloadedFile, failure: Failure | None = None) -> Case[DownloadedFile]:
    return Case.branch_result(DOWNLOADED, value, failure)

setattr(download_file_downloaded, '__module__', "dex_connectors.dropbox.dropboxmock")

def download_file_not_found(value: DownloadedFile, failure: Failure | None = None) -> Case[DownloadedFile]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(download_file_not_found, '__module__', "dex_connectors.dropbox.dropboxmock")

def download_file_too_large(value: DownloadedFile, failure: Failure | None = None) -> Case[DownloadedFile]:
    return Case.branch_result(TOO_LARGE, value, failure)

setattr(download_file_too_large, '__module__', "dex_connectors.dropbox.dropboxmock")

def download_file_provider_rejected(value: DownloadedFile, failure: Failure | None = None) -> Case[DownloadedFile]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(download_file_provider_rejected, '__module__', "dex_connectors.dropbox.dropboxmock")

def download_file_invalid_response(value: DownloadedFile, failure: Failure | None = None) -> Case[DownloadedFile]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(download_file_invalid_response, '__module__', "dex_connectors.dropbox.dropboxmock")

def download_file_defect(value: DownloadedFile, failure: Failure | None = None) -> Case[DownloadedFile]:
    return Case.branch_result(DEFECT, value, failure)

setattr(download_file_defect, '__module__', "dex_connectors.dropbox.dropboxmock")

def download_file_retry(failure: Failure, after_seconds: float = 0) -> Case[DownloadedFile]:
    return Case.retry(failure, after_seconds)

setattr(download_file_retry, '__module__', "dex_connectors.dropbox.dropboxmock")

def download_file_downloaded_default() -> Case[DownloadedFile]:
    return Case.branch_result(DOWNLOADED, DownloadedFile(file=EntryMetadata(id='id:example', name='example.txt', path_display='/example.txt', kind='file'), content_base64='SGVsbG8=', content_bytes=5))

setattr(download_file_downloaded_default, '__module__', "dex_connectors.dropbox.dropboxmock")

def upload_file_uploaded(value: UploadedFile, failure: Failure | None = None) -> Case[UploadedFile]:
    return Case.branch_result(UPLOADED, value, failure)

setattr(upload_file_uploaded, '__module__', "dex_connectors.dropbox.dropboxmock")

def upload_file_provider_rejected(value: UploadedFile, failure: Failure | None = None) -> Case[UploadedFile]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(upload_file_provider_rejected, '__module__', "dex_connectors.dropbox.dropboxmock")

def upload_file_invalid_response(value: UploadedFile, failure: Failure | None = None) -> Case[UploadedFile]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(upload_file_invalid_response, '__module__', "dex_connectors.dropbox.dropboxmock")

def upload_file_defect(value: UploadedFile, failure: Failure | None = None) -> Case[UploadedFile]:
    return Case.branch_result(DEFECT, value, failure)

setattr(upload_file_defect, '__module__', "dex_connectors.dropbox.dropboxmock")

def upload_file_retry(failure: Failure, after_seconds: float = 0) -> Case[UploadedFile]:
    return Case.retry(failure, after_seconds)

setattr(upload_file_retry, '__module__', "dex_connectors.dropbox.dropboxmock")

def upload_file_uploaded_default() -> Case[UploadedFile]:
    return Case.branch_result(UPLOADED, UploadedFile(file=EntryMetadata(id='id:example', name='example.txt', path_display='/example.txt', kind='file')))

setattr(upload_file_uploaded_default, '__module__', "dex_connectors.dropbox.dropboxmock")


class Mock:

    def __init__(self, connection_name: str = "dropbox") -> None:
        self.list_folder_script: Query[ListFolderInput, FolderPage] = Query(LIST_FOLDER_DEFINITION)
        self.list_folder_script.default(list_folder_listed_default())
        self.get_metadata_script: Query[GetMetadataInput, EntryMetadata] = Query(GET_METADATA_DEFINITION)
        self.get_metadata_script.default(get_metadata_found_default())
        self.continue_folder_script: Query[ContinueFolderInput, FolderPage] = Query(CONTINUE_FOLDER_DEFINITION)
        self.continue_folder_script.default(continue_folder_listed_default())
        self.download_file_script: Query[DownloadFileInput, DownloadedFile] = Query(DOWNLOAD_FILE_DEFINITION)
        self.download_file_script.default(download_file_downloaded_default())
        self.upload_file_script: Mutation[UploadFileInput, UploadedFile] = Mutation(UPLOAD_FILE_DEFINITION)
        self.upload_file_script.default(upload_file_uploaded_default())
        self._connection = new_connection_with_operations(
            list_folder=self.list_folder_script,
            get_metadata=self.get_metadata_script,
            continue_folder=self.continue_folder_script,
            download_file=self.download_file_script,
            upload_file=self.upload_file_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.dropbox.dropboxmock")

__all__ = [
    "Mock",
    "list_folder_listed",
    "list_folder_not_found",
    "list_folder_provider_rejected",
    "list_folder_invalid_response",
    "list_folder_defect",
    "list_folder_retry",
    "list_folder_listed_default",
    "get_metadata_found",
    "get_metadata_not_found",
    "get_metadata_provider_rejected",
    "get_metadata_invalid_response",
    "get_metadata_defect",
    "get_metadata_retry",
    "get_metadata_found_default",
    "continue_folder_listed",
    "continue_folder_cursor_expired",
    "continue_folder_provider_rejected",
    "continue_folder_invalid_response",
    "continue_folder_defect",
    "continue_folder_retry",
    "continue_folder_listed_default",
    "download_file_downloaded",
    "download_file_not_found",
    "download_file_too_large",
    "download_file_provider_rejected",
    "download_file_invalid_response",
    "download_file_defect",
    "download_file_retry",
    "download_file_downloaded_default",
    "upload_file_uploaded",
    "upload_file_provider_rejected",
    "upload_file_invalid_response",
    "upload_file_defect",
    "upload_file_retry",
    "upload_file_uploaded_default",
]
