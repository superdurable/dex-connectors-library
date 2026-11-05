# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CreateFolderInput, CreateFolderOutput, DriveItem, FileSummary, GetFileInput, ItemIdentity, ReadFileTextInput, ReadFileTextOutput, SearchFilesInput, SearchFilesOutput, UploadFileInput, UploadFileOutput
from .definitions import CONNECTOR_ID, PROVIDER, ALREADY_EXISTS, CREATED, CREATE_FOLDER_DEFINITION, DEFECT, FOUND, GET_FILE_DEFINITION, INVALID_RESPONSE, NAME_CONFLICT, NOT_FOUND, PROVIDER_REJECTED, READ, READ_FILE_TEXT_DEFINITION, SEARCH_FILES_DEFINITION, TOO_LARGE, UNSUPPORTED_CONTENT, UPLOADED, UPLOAD_FILE_DEFINITION

def search_files_found(value: SearchFilesOutput, failure: Failure | None = None) -> Case[SearchFilesOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(search_files_found, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def search_files_not_found(value: SearchFilesOutput, failure: Failure | None = None) -> Case[SearchFilesOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(search_files_not_found, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def search_files_provider_rejected(value: SearchFilesOutput, failure: Failure | None = None) -> Case[SearchFilesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_files_provider_rejected, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def search_files_invalid_response(value: SearchFilesOutput, failure: Failure | None = None) -> Case[SearchFilesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_files_invalid_response, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def search_files_defect(value: SearchFilesOutput, failure: Failure | None = None) -> Case[SearchFilesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_files_defect, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def search_files_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchFilesOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_files_retry, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def search_files_found_default_answer() -> Case[SearchFilesOutput]:
    return Case.branch_result(FOUND, SearchFilesOutput(files=(FileSummary(id='source-file-1', name='Ops Policy.txt', drive_id='b!sourceLibrary_1', parent_folder_id='source-folder', mime_type='text/plain', size_bytes=44),), next_page_token=''))

setattr(search_files_found_default_answer, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def search_files_missing_name() -> Case[SearchFilesOutput]:
    return Case.branch_result(NOT_FOUND, SearchFilesOutput(files=()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "searchFiles", "Searching for Missing Policy.txt returns no matching item."))

setattr(search_files_missing_name, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def search_files_restricted_folder() -> Case[SearchFilesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SearchFilesOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "searchFiles", "Searching source-folder without read permission is rejected."))

setattr(search_files_restricted_folder, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def get_file_found(value: DriveItem, failure: Failure | None = None) -> Case[DriveItem]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_file_found, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def get_file_not_found(value: DriveItem, failure: Failure | None = None) -> Case[DriveItem]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_file_not_found, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def get_file_provider_rejected(value: DriveItem, failure: Failure | None = None) -> Case[DriveItem]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_file_provider_rejected, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def get_file_invalid_response(value: DriveItem, failure: Failure | None = None) -> Case[DriveItem]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_file_invalid_response, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def get_file_defect(value: DriveItem, failure: Failure | None = None) -> Case[DriveItem]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_file_defect, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def get_file_retry(failure: Failure, after_seconds: float = 0) -> Case[DriveItem]:
    return Case.retry(failure, after_seconds)

setattr(get_file_retry, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def get_file_found_default_answer() -> Case[DriveItem]:
    return Case.branch_result(FOUND, DriveItem(id='copy-file-1', name='Ops Policy (copy).txt', drive_id='b!financeLibrary-2', parent_folder_id='copy-folder-1', mime_type='text/plain', size_bytes=44))

setattr(get_file_found_default_answer, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def get_file_missing_item() -> Case[DriveItem]:
    return Case.branch_result(NOT_FOUND, DriveItem(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getFile", "Reading missing-file-1 returns not found."))

setattr(get_file_missing_item, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def get_file_denied_read() -> Case[DriveItem]:
    return Case.branch_result(PROVIDER_REJECTED, DriveItem(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getFile", "Reading copy-file-1 metadata without Graph permission is rejected."))

setattr(get_file_denied_read, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def read_file_text_read(value: ReadFileTextOutput, failure: Failure | None = None) -> Case[ReadFileTextOutput]:
    return Case.branch_result(READ, value, failure)

setattr(read_file_text_read, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def read_file_text_not_found(value: ReadFileTextOutput, failure: Failure | None = None) -> Case[ReadFileTextOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(read_file_text_not_found, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def read_file_text_unsupported_content(value: ReadFileTextOutput, failure: Failure | None = None) -> Case[ReadFileTextOutput]:
    return Case.branch_result(UNSUPPORTED_CONTENT, value, failure)

setattr(read_file_text_unsupported_content, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def read_file_text_too_large(value: ReadFileTextOutput, failure: Failure | None = None) -> Case[ReadFileTextOutput]:
    return Case.branch_result(TOO_LARGE, value, failure)

setattr(read_file_text_too_large, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def read_file_text_provider_rejected(value: ReadFileTextOutput, failure: Failure | None = None) -> Case[ReadFileTextOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(read_file_text_provider_rejected, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def read_file_text_invalid_response(value: ReadFileTextOutput, failure: Failure | None = None) -> Case[ReadFileTextOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(read_file_text_invalid_response, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def read_file_text_defect(value: ReadFileTextOutput, failure: Failure | None = None) -> Case[ReadFileTextOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(read_file_text_defect, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def read_file_text_retry(failure: Failure, after_seconds: float = 0) -> Case[ReadFileTextOutput]:
    return Case.retry(failure, after_seconds)

setattr(read_file_text_retry, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def read_file_text_read_default_answer() -> Case[ReadFileTextOutput]:
    return Case.branch_result(READ, ReadFileTextOutput(item_id='source-file-1', name='Ops Policy.txt', mime_type='text/plain', size_bytes=44, text='Ops Policy\nRefunds above 500 need approval.\n', byte_count=44))

setattr(read_file_text_read_default_answer, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def read_file_text_missing_item() -> Case[ReadFileTextOutput]:
    return Case.branch_result(NOT_FOUND, ReadFileTextOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "readFileText", "Reading missing-file-1 returns not found."))

setattr(read_file_text_missing_item, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def read_file_text_folder_content() -> Case[ReadFileTextOutput]:
    return Case.branch_result(UNSUPPORTED_CONTENT, ReadFileTextOutput(item_id='source-folder', name='Policies', size_bytes=44, byte_count=0), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "readFileText", "Reading source-folder as UTF-8 text is unsupported."))

setattr(read_file_text_folder_content, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def read_file_text_oversized_text() -> Case[ReadFileTextOutput]:
    return Case.branch_result(TOO_LARGE, ReadFileTextOutput(item_id='source-file-large', name='Archive.txt', mime_type='text/plain', size_bytes=2097152, byte_count=0), Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "readFileText", "Reading source-file-large over the configured text limit is rejected."))

setattr(read_file_text_oversized_text, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def read_file_text_denied_download() -> Case[ReadFileTextOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ReadFileTextOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "readFileText", "Reading source-file-1 metadata when Graph returns 403 is rejected."))

setattr(read_file_text_denied_download, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def upload_file_uploaded(value: UploadFileOutput, failure: Failure | None = None) -> Case[UploadFileOutput]:
    return Case.branch_result(UPLOADED, value, failure)

setattr(upload_file_uploaded, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def upload_file_already_exists(value: UploadFileOutput, failure: Failure | None = None) -> Case[UploadFileOutput]:
    return Case.branch_result(ALREADY_EXISTS, value, failure)

setattr(upload_file_already_exists, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def upload_file_provider_rejected(value: UploadFileOutput, failure: Failure | None = None) -> Case[UploadFileOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(upload_file_provider_rejected, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def upload_file_invalid_response(value: UploadFileOutput, failure: Failure | None = None) -> Case[UploadFileOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(upload_file_invalid_response, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def upload_file_defect(value: UploadFileOutput, failure: Failure | None = None) -> Case[UploadFileOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(upload_file_defect, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def upload_file_retry(failure: Failure, after_seconds: float = 0) -> Case[UploadFileOutput]:
    return Case.retry(failure, after_seconds)

setattr(upload_file_retry, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def upload_file_uploaded_default_answer() -> Case[UploadFileOutput]:
    return Case.branch_result(UPLOADED, UploadFileOutput(item=DriveItem(id='copy-file-1', name='Ops Policy (copy).txt', drive_id='b!financeLibrary-2', parent_folder_id='copy-folder-1', mime_type='text/plain', size_bytes=44)))

setattr(upload_file_uploaded_default_answer, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def upload_file_different_file() -> Case[UploadFileOutput]:
    return Case.branch_result(ALREADY_EXISTS, UploadFileOutput(item=DriveItem(id='existing-copy', name='Ops Policy (copy).txt', drive_id='b!financeLibrary-2', parent_folder_id='copy-folder-1', mime_type='text/plain', size_bytes=12, quick_xor_hash='T6ADGsogBxCYIQ1sJIMLFAAAAAA=')), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "uploadFile", "Uploading Ops Policy (copy).txt with fail behavior over existing-copy returns a conflict."))

setattr(upload_file_different_file, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def upload_file_quota_exhausted() -> Case[UploadFileOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UploadFileOutput(item=DriveItem()), Failure(FailureKind.QUOTA_EXHAUSTED, CONNECTOR_ID, "uploadFile", "Uploading Ops Policy (copy).txt into copy-folder-1 when the drive quota is exhausted is rejected."))

setattr(upload_file_quota_exhausted, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def create_folder_created(value: CreateFolderOutput, failure: Failure | None = None) -> Case[CreateFolderOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_folder_created, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def create_folder_name_conflict(value: CreateFolderOutput, failure: Failure | None = None) -> Case[CreateFolderOutput]:
    return Case.branch_result(NAME_CONFLICT, value, failure)

setattr(create_folder_name_conflict, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def create_folder_provider_rejected(value: CreateFolderOutput, failure: Failure | None = None) -> Case[CreateFolderOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_folder_provider_rejected, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def create_folder_invalid_response(value: CreateFolderOutput, failure: Failure | None = None) -> Case[CreateFolderOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(create_folder_invalid_response, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def create_folder_defect(value: CreateFolderOutput, failure: Failure | None = None) -> Case[CreateFolderOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_folder_defect, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def create_folder_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateFolderOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_folder_retry, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def create_folder_created_default_answer() -> Case[CreateFolderOutput]:
    return Case.branch_result(CREATED, CreateFolderOutput(folder=DriveItem(id='copy-folder-1', name='Dex copies', drive_id='b!financeLibrary-2', parent_folder_id='destination-folder', is_folder=True)))

setattr(create_folder_created_default_answer, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def create_folder_file_at_folder_name() -> Case[CreateFolderOutput]:
    return Case.branch_result(NAME_CONFLICT, CreateFolderOutput(folder=DriveItem(id='existing-file', name='Dex copies', drive_id='b!financeLibrary-2', parent_folder_id='destination-folder', mime_type='text/plain', size_bytes=12)), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "createFolder", "Creating Dex copies in destination-folder where existing-file has that name returns a conflict."))

setattr(create_folder_file_at_folder_name, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

def create_folder_read_only_parent() -> Case[CreateFolderOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateFolderOutput(folder=DriveItem()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "createFolder", "Creating Dex copies in read-only destination-folder is rejected."))

setattr(create_folder_read_only_parent, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")


class Mock:

    def __init__(self, connection_name: str = "microsoft-onedrive") -> None:
        self.search_files_script: Query[SearchFilesInput, SearchFilesOutput] = Query(SEARCH_FILES_DEFINITION)
        self.search_files_script.default(search_files_found_default_answer())
        self.get_file_script: Query[GetFileInput, DriveItem] = Query(GET_FILE_DEFINITION)
        self.get_file_script.default(get_file_found_default_answer())
        self.read_file_text_script: Query[ReadFileTextInput, ReadFileTextOutput] = Query(READ_FILE_TEXT_DEFINITION)
        self.read_file_text_script.default(read_file_text_read_default_answer())
        self.upload_file_script: Mutation[UploadFileInput, UploadFileOutput] = Mutation(UPLOAD_FILE_DEFINITION)
        self.upload_file_script.default(upload_file_uploaded_default_answer())
        self.create_folder_script: Mutation[CreateFolderInput, CreateFolderOutput] = Mutation(CREATE_FOLDER_DEFINITION)
        self.create_folder_script.default(create_folder_created_default_answer())
        self._connection = new_connection_with_operations(
            search_files=self.search_files_script,
            get_file=self.get_file_script,
            read_file_text=self.read_file_text_script,
            upload_file=self.upload_file_script,
            create_folder=self.create_folder_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.microsoft_onedrive.onedrivemock")

__all__ = [
    "Mock",
    "search_files_found",
    "search_files_not_found",
    "search_files_provider_rejected",
    "search_files_invalid_response",
    "search_files_defect",
    "search_files_retry",
    "search_files_found_default_answer",
    "search_files_missing_name",
    "search_files_restricted_folder",
    "get_file_found",
    "get_file_not_found",
    "get_file_provider_rejected",
    "get_file_invalid_response",
    "get_file_defect",
    "get_file_retry",
    "get_file_found_default_answer",
    "get_file_missing_item",
    "get_file_denied_read",
    "read_file_text_read",
    "read_file_text_not_found",
    "read_file_text_unsupported_content",
    "read_file_text_too_large",
    "read_file_text_provider_rejected",
    "read_file_text_invalid_response",
    "read_file_text_defect",
    "read_file_text_retry",
    "read_file_text_read_default_answer",
    "read_file_text_missing_item",
    "read_file_text_folder_content",
    "read_file_text_oversized_text",
    "read_file_text_denied_download",
    "upload_file_uploaded",
    "upload_file_already_exists",
    "upload_file_provider_rejected",
    "upload_file_invalid_response",
    "upload_file_defect",
    "upload_file_retry",
    "upload_file_uploaded_default_answer",
    "upload_file_different_file",
    "upload_file_quota_exhausted",
    "create_folder_created",
    "create_folder_name_conflict",
    "create_folder_provider_rejected",
    "create_folder_invalid_response",
    "create_folder_defect",
    "create_folder_retry",
    "create_folder_created_default_answer",
    "create_folder_file_at_folder_name",
    "create_folder_read_only_parent",
]
