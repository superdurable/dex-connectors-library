# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import File, FileSummary, FileUser, GetFileInput, ReadFileTextInput, ReadFileTextOutput, SearchFilesInput, SearchFilesOutput, ShortcutTarget, UploadFileInput, UploadFileOutput
from .definitions import CONNECTOR_ID, PROVIDER, DEFECT, FOUND, GET_FILE_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, READ, READ_FILE_TEXT_DEFINITION, SEARCH_FILES_DEFINITION, TOO_LARGE, UNCERTAIN, UNSUPPORTED_CONTENT, UPLOADED, UPLOAD_FILE_DEFINITION

def search_files_found(value: SearchFilesOutput, failure: Failure | None = None) -> Case[SearchFilesOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(search_files_found, '__module__', "dex_connectors.google_drive.drivemock")

def search_files_not_found(value: SearchFilesOutput, failure: Failure | None = None) -> Case[SearchFilesOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(search_files_not_found, '__module__', "dex_connectors.google_drive.drivemock")

def search_files_provider_rejected(value: SearchFilesOutput, failure: Failure | None = None) -> Case[SearchFilesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_files_provider_rejected, '__module__', "dex_connectors.google_drive.drivemock")

def search_files_invalid_response(value: SearchFilesOutput, failure: Failure | None = None) -> Case[SearchFilesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_files_invalid_response, '__module__', "dex_connectors.google_drive.drivemock")

def search_files_defect(value: SearchFilesOutput, failure: Failure | None = None) -> Case[SearchFilesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_files_defect, '__module__', "dex_connectors.google_drive.drivemock")

def search_files_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchFilesOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_files_retry, '__module__', "dex_connectors.google_drive.drivemock")

def search_files_found_default_answer() -> Case[SearchFilesOutput]:
    return Case.paged(FOUND, (SearchFilesOutput(files=(FileSummary(id='file_recent', name='Current Plan', mime_type='text/plain'), FileSummary(id='file_archive', name='Archive 2025', mime_type='text/plain')), next_page_token='next-2'), SearchFilesOutput(files=(FileSummary(id='file_archive', name='Archive 2025', mime_type='text/plain'), FileSummary(id='file_policy', name='Ops Policy', mime_type='application/vnd.google-apps.document')), next_page_token='next-3'), SearchFilesOutput(files=(FileSummary(id='file_notes', name='Notes', mime_type='text/plain'),))))

setattr(search_files_found_default_answer, '__module__', "dex_connectors.google_drive.drivemock")

def search_files_not_found_failure() -> Case[SearchFilesOutput]:
    return Case.branch_result(NOT_FOUND, SearchFilesOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "searchFiles", "No file matched and Google Drive reported no further pages."))

setattr(search_files_not_found_failure, '__module__', "dex_connectors.google_drive.drivemock")

def search_files_provider_rejected_failure() -> Case[SearchFilesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SearchFilesOutput(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "searchFiles", "Google rejected the expired search page token."))

setattr(search_files_provider_rejected_failure, '__module__', "dex_connectors.google_drive.drivemock")

def search_files_invalid_response_failure() -> Case[SearchFilesOutput]:
    return Case.branch_result(INVALID_RESPONSE, SearchFilesOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "searchFiles", "Google returned malformed search metadata."))

setattr(search_files_invalid_response_failure, '__module__', "dex_connectors.google_drive.drivemock")

def get_file_found(value: File, failure: Failure | None = None) -> Case[File]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_file_found, '__module__', "dex_connectors.google_drive.drivemock")

def get_file_not_found(value: File, failure: Failure | None = None) -> Case[File]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_file_not_found, '__module__', "dex_connectors.google_drive.drivemock")

def get_file_provider_rejected(value: File, failure: Failure | None = None) -> Case[File]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_file_provider_rejected, '__module__', "dex_connectors.google_drive.drivemock")

def get_file_invalid_response(value: File, failure: Failure | None = None) -> Case[File]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_file_invalid_response, '__module__', "dex_connectors.google_drive.drivemock")

def get_file_defect(value: File, failure: Failure | None = None) -> Case[File]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_file_defect, '__module__', "dex_connectors.google_drive.drivemock")

def get_file_retry(failure: Failure, after_seconds: float = 0) -> Case[File]:
    return Case.retry(failure, after_seconds)

setattr(get_file_retry, '__module__', "dex_connectors.google_drive.drivemock")

def get_file_found_default_answer() -> Case[File]:
    return Case.branch_result(FOUND, File(id='copied_policy', name='Ops Policy copy.txt', mime_type='text/plain', parents=('fld_finance',), size_bytes=43, web_view_link='https://drive.google.com/file/d/copied_policy/view'))

setattr(get_file_found_default_answer, '__module__', "dex_connectors.google_drive.drivemock")

def get_file_not_found_failure() -> Case[File]:
    return Case.branch_result(NOT_FOUND, File(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getFile", "The file does not exist or the connection cannot see it."))

setattr(get_file_not_found_failure, '__module__', "dex_connectors.google_drive.drivemock")

def get_file_provider_rejected_failure() -> Case[File]:
    return Case.branch_result(PROVIDER_REJECTED, File(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "getFile", "Google rejected the metadata read."))

setattr(get_file_provider_rejected_failure, '__module__', "dex_connectors.google_drive.drivemock")

def get_file_invalid_response_failure() -> Case[File]:
    return Case.branch_result(INVALID_RESPONSE, File(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getFile", "Google returned malformed file metadata."))

setattr(get_file_invalid_response_failure, '__module__', "dex_connectors.google_drive.drivemock")

def read_file_text_read(value: ReadFileTextOutput, failure: Failure | None = None) -> Case[ReadFileTextOutput]:
    return Case.branch_result(READ, value, failure)

setattr(read_file_text_read, '__module__', "dex_connectors.google_drive.drivemock")

def read_file_text_not_found(value: ReadFileTextOutput, failure: Failure | None = None) -> Case[ReadFileTextOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(read_file_text_not_found, '__module__', "dex_connectors.google_drive.drivemock")

def read_file_text_unsupported_content(value: ReadFileTextOutput, failure: Failure | None = None) -> Case[ReadFileTextOutput]:
    return Case.branch_result(UNSUPPORTED_CONTENT, value, failure)

setattr(read_file_text_unsupported_content, '__module__', "dex_connectors.google_drive.drivemock")

def read_file_text_too_large(value: ReadFileTextOutput, failure: Failure | None = None) -> Case[ReadFileTextOutput]:
    return Case.branch_result(TOO_LARGE, value, failure)

setattr(read_file_text_too_large, '__module__', "dex_connectors.google_drive.drivemock")

def read_file_text_provider_rejected(value: ReadFileTextOutput, failure: Failure | None = None) -> Case[ReadFileTextOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(read_file_text_provider_rejected, '__module__', "dex_connectors.google_drive.drivemock")

def read_file_text_invalid_response(value: ReadFileTextOutput, failure: Failure | None = None) -> Case[ReadFileTextOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(read_file_text_invalid_response, '__module__', "dex_connectors.google_drive.drivemock")

def read_file_text_defect(value: ReadFileTextOutput, failure: Failure | None = None) -> Case[ReadFileTextOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(read_file_text_defect, '__module__', "dex_connectors.google_drive.drivemock")

def read_file_text_retry(failure: Failure, after_seconds: float = 0) -> Case[ReadFileTextOutput]:
    return Case.retry(failure, after_seconds)

setattr(read_file_text_retry, '__module__', "dex_connectors.google_drive.drivemock")

def read_file_text_read_default_answer() -> Case[ReadFileTextOutput]:
    return Case.branch_result(READ, ReadFileTextOutput(file_id='file_policy', name='Ops Policy', mime_type='application/vnd.google-apps.document', text_mime_type='text/plain', text='Ops Policy\nRefunds above 500 need approval.', byte_count=43))

setattr(read_file_text_read_default_answer, '__module__', "dex_connectors.google_drive.drivemock")

def read_file_text_not_found_failure() -> Case[ReadFileTextOutput]:
    return Case.branch_result(NOT_FOUND, ReadFileTextOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "readFileText", "The file does not exist or the connection cannot see it."))

setattr(read_file_text_not_found_failure, '__module__', "dex_connectors.google_drive.drivemock")

def read_file_text_unsupported_content_failure() -> Case[ReadFileTextOutput]:
    return Case.branch_result(UNSUPPORTED_CONTENT, ReadFileTextOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "readFileText", "The file cannot be read as UTF-8 text."))

setattr(read_file_text_unsupported_content_failure, '__module__', "dex_connectors.google_drive.drivemock")

def read_file_text_too_large_failure() -> Case[ReadFileTextOutput]:
    return Case.branch_result(TOO_LARGE, ReadFileTextOutput(), Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "readFileText", "The file text exceeds the configured limit or Google's export limit."))

setattr(read_file_text_too_large_failure, '__module__', "dex_connectors.google_drive.drivemock")

def read_file_text_provider_rejected_failure() -> Case[ReadFileTextOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ReadFileTextOutput(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "readFileText", "Google rejected the content download."))

setattr(read_file_text_provider_rejected_failure, '__module__', "dex_connectors.google_drive.drivemock")

def read_file_text_invalid_response_failure() -> Case[ReadFileTextOutput]:
    return Case.branch_result(INVALID_RESPONSE, ReadFileTextOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "readFileText", "Google returned malformed metadata."))

setattr(read_file_text_invalid_response_failure, '__module__', "dex_connectors.google_drive.drivemock")

def upload_file_uploaded(value: UploadFileOutput, failure: Failure | None = None) -> Case[UploadFileOutput]:
    return Case.branch_result(UPLOADED, value, failure)

setattr(upload_file_uploaded, '__module__', "dex_connectors.google_drive.drivemock")

def upload_file_provider_rejected(value: UploadFileOutput, failure: Failure | None = None) -> Case[UploadFileOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(upload_file_provider_rejected, '__module__', "dex_connectors.google_drive.drivemock")

def upload_file_invalid_response(value: UploadFileOutput, failure: Failure | None = None) -> Case[UploadFileOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(upload_file_invalid_response, '__module__', "dex_connectors.google_drive.drivemock")

def upload_file_uncertain(value: UploadFileOutput, failure: Failure) -> Case[UploadFileOutput]:
    return Case.uncertain(value, failure)

setattr(upload_file_uncertain, '__module__', "dex_connectors.google_drive.drivemock")

def upload_file_defect(value: UploadFileOutput, failure: Failure | None = None) -> Case[UploadFileOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(upload_file_defect, '__module__', "dex_connectors.google_drive.drivemock")

def upload_file_retry(failure: Failure, after_seconds: float = 0) -> Case[UploadFileOutput]:
    return Case.retry(failure, after_seconds)

setattr(upload_file_retry, '__module__', "dex_connectors.google_drive.drivemock")

def upload_file_uploaded_default_answer() -> Case[UploadFileOutput]:
    return Case.branch_result(UPLOADED, UploadFileOutput(file=File(id='copied_policy', name='Ops Policy copy.txt', mime_type='text/plain', parents=('fld_finance',), size_bytes=43)))

setattr(upload_file_uploaded_default_answer, '__module__', "dex_connectors.google_drive.drivemock")

def upload_file_provider_rejected_failure() -> Case[UploadFileOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UploadFileOutput(file=File()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "uploadFile", "The parent folder does not exist or is not visible."))

setattr(upload_file_provider_rejected_failure, '__module__', "dex_connectors.google_drive.drivemock")

def upload_file_invalid_response_failure() -> Case[UploadFileOutput]:
    return Case.branch_result(INVALID_RESPONSE, UploadFileOutput(file=File()), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "uploadFile", "Google returned malformed duplicate-lookup metadata."))

setattr(upload_file_invalid_response_failure, '__module__', "dex_connectors.google_drive.drivemock")


class Mock:

    def __init__(self, connection_name: str = "google-drive") -> None:
        self.search_files_script: Query[SearchFilesInput, SearchFilesOutput] = Query(SEARCH_FILES_DEFINITION)
        self.search_files_script.default(search_files_found_default_answer())
        self.get_file_script: Query[GetFileInput, File] = Query(GET_FILE_DEFINITION)
        self.get_file_script.default(get_file_found_default_answer())
        self.read_file_text_script: Query[ReadFileTextInput, ReadFileTextOutput] = Query(READ_FILE_TEXT_DEFINITION)
        self.read_file_text_script.default(read_file_text_read_default_answer())
        self.upload_file_script: Mutation[UploadFileInput, UploadFileOutput] = Mutation(UPLOAD_FILE_DEFINITION)
        self.upload_file_script.default(upload_file_uploaded_default_answer())
        self._connection = new_connection_with_operations(
            search_files=self.search_files_script,
            get_file=self.get_file_script,
            read_file_text=self.read_file_text_script,
            upload_file=self.upload_file_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.google_drive.drivemock")

__all__ = [
    "Mock",
    "search_files_found",
    "search_files_not_found",
    "search_files_provider_rejected",
    "search_files_invalid_response",
    "search_files_defect",
    "search_files_retry",
    "search_files_found_default_answer",
    "search_files_not_found_failure",
    "search_files_provider_rejected_failure",
    "search_files_invalid_response_failure",
    "get_file_found",
    "get_file_not_found",
    "get_file_provider_rejected",
    "get_file_invalid_response",
    "get_file_defect",
    "get_file_retry",
    "get_file_found_default_answer",
    "get_file_not_found_failure",
    "get_file_provider_rejected_failure",
    "get_file_invalid_response_failure",
    "read_file_text_read",
    "read_file_text_not_found",
    "read_file_text_unsupported_content",
    "read_file_text_too_large",
    "read_file_text_provider_rejected",
    "read_file_text_invalid_response",
    "read_file_text_defect",
    "read_file_text_retry",
    "read_file_text_read_default_answer",
    "read_file_text_not_found_failure",
    "read_file_text_unsupported_content_failure",
    "read_file_text_too_large_failure",
    "read_file_text_provider_rejected_failure",
    "read_file_text_invalid_response_failure",
    "upload_file_uploaded",
    "upload_file_provider_rejected",
    "upload_file_invalid_response",
    "upload_file_uncertain",
    "upload_file_defect",
    "upload_file_retry",
    "upload_file_uploaded_default_answer",
    "upload_file_provider_rejected_failure",
    "upload_file_invalid_response_failure",
]
