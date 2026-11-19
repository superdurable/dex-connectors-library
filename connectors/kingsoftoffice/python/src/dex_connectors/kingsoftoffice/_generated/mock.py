# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import FileMetadata, ListFolderFilesInput, ListFolderFilesOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_FOLDER_FILES_DEFINITION, PROVIDER_REJECTED

def list_folder_files_listed(value: ListFolderFilesOutput, failure: Failure | None = None) -> Case[ListFolderFilesOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_folder_files_listed, '__module__', "dex_connectors.kingsoftoffice.kingsoftofficemock")

def list_folder_files_provider_rejected(value: ListFolderFilesOutput, failure: Failure | None = None) -> Case[ListFolderFilesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_folder_files_provider_rejected, '__module__', "dex_connectors.kingsoftoffice.kingsoftofficemock")

def list_folder_files_invalid_response(value: ListFolderFilesOutput, failure: Failure | None = None) -> Case[ListFolderFilesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_folder_files_invalid_response, '__module__', "dex_connectors.kingsoftoffice.kingsoftofficemock")

def list_folder_files_defect(value: ListFolderFilesOutput, failure: Failure | None = None) -> Case[ListFolderFilesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_folder_files_defect, '__module__', "dex_connectors.kingsoftoffice.kingsoftofficemock")

def list_folder_files_retry(failure: Failure, after_seconds: float = 0) -> Case[ListFolderFilesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_folder_files_retry, '__module__', "dex_connectors.kingsoftoffice.kingsoftofficemock")

def list_folder_files_sample_folder() -> Case[ListFolderFilesOutput]:
    return Case.branch_result(LISTED, ListFolderFilesOutput(items=(FileMetadata(id='file_sample_1', name='Example.txt', type='file', size_bytes=12),), next_page_token=''))

setattr(list_folder_files_sample_folder, '__module__', "dex_connectors.kingsoftoffice.kingsoftofficemock")


class Mock:

    def __init__(self, connection_name: str = "kingsoftoffice") -> None:
        self.list_folder_files_script: Query[ListFolderFilesInput, ListFolderFilesOutput] = Query(LIST_FOLDER_FILES_DEFINITION)
        self.list_folder_files_script.default(list_folder_files_sample_folder())
        self._connection = new_connection_with_operations(
            list_folder_files=self.list_folder_files_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.kingsoftoffice.kingsoftofficemock")

__all__ = [
    "Mock",
    "list_folder_files_listed",
    "list_folder_files_provider_rejected",
    "list_folder_files_invalid_response",
    "list_folder_files_defect",
    "list_folder_files_retry",
    "list_folder_files_sample_folder",
]
