# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated manifest-backed provider outcomes and scripts."""

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import File, FileComment, FileComments, GetFileCommentsInput, GetFileInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_FILE_COMMENTS_DEFINITION, GET_FILE_DEFINITION, INVALID_RESPONSE, LISTED, NOT_FOUND, PROVIDER_REJECTED

def get_file_found(value: File, failure: Failure | None = None) -> Case[File]:
    """Script the found branch."""
    return Case.branch_result(FOUND, value, failure)

setattr(get_file_found, '__module__', "dex_connectors.figma.figmamock")

def get_file_not_found(value: File, failure: Failure | None = None) -> Case[File]:
    """Script the notFound branch."""
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_file_not_found, '__module__', "dex_connectors.figma.figmamock")

def get_file_provider_rejected(value: File, failure: Failure | None = None) -> Case[File]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_file_provider_rejected, '__module__', "dex_connectors.figma.figmamock")

def get_file_invalid_response(value: File, failure: Failure | None = None) -> Case[File]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_file_invalid_response, '__module__', "dex_connectors.figma.figmamock")

def get_file_defect(value: File, failure: Failure | None = None) -> Case[File]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(get_file_defect, '__module__', "dex_connectors.figma.figmamock")

def get_file_retry(failure: Failure, after_seconds: float = 0) -> Case[File]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(get_file_retry, '__module__', "dex_connectors.figma.figmamock")

def get_file_default_answer() -> Case[File]:
    """Return the manifest defaultAnswer outcome."""
    return Case.branch_result(FOUND, File(name='Meridian Design', last_modified='2026-10-28T12:00:00Z', document={'id': '0:0', 'type': 'DOCUMENT', 'name': 'Root'}))

setattr(get_file_default_answer, '__module__', "dex_connectors.figma.figmamock")

def get_file_invalid_key() -> Case[File]:
    """Return the manifest invalidKey outcome."""
    return Case.branch_result(PROVIDER_REJECTED, File(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getFile", "Figma rejected the token."))

setattr(get_file_invalid_key, '__module__', "dex_connectors.figma.figmamock")

def get_file_comments_listed(value: FileComments, failure: Failure | None = None) -> Case[FileComments]:
    """Script the listed branch."""
    return Case.branch_result(LISTED, value, failure)

setattr(get_file_comments_listed, '__module__', "dex_connectors.figma.figmamock")

def get_file_comments_not_found(value: FileComments, failure: Failure | None = None) -> Case[FileComments]:
    """Script the notFound branch."""
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_file_comments_not_found, '__module__', "dex_connectors.figma.figmamock")

def get_file_comments_provider_rejected(value: FileComments, failure: Failure | None = None) -> Case[FileComments]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_file_comments_provider_rejected, '__module__', "dex_connectors.figma.figmamock")

def get_file_comments_invalid_response(value: FileComments, failure: Failure | None = None) -> Case[FileComments]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_file_comments_invalid_response, '__module__', "dex_connectors.figma.figmamock")

def get_file_comments_defect(value: FileComments, failure: Failure | None = None) -> Case[FileComments]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(get_file_comments_defect, '__module__', "dex_connectors.figma.figmamock")

def get_file_comments_retry(failure: Failure, after_seconds: float = 0) -> Case[FileComments]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(get_file_comments_retry, '__module__', "dex_connectors.figma.figmamock")

def get_file_comments_default_answer() -> Case[FileComments]:
    """Return the manifest defaultAnswer outcome."""
    return Case.branch_result(LISTED, FileComments(comments=(FileComment(id='1', message='Review the header.'),)))

setattr(get_file_comments_default_answer, '__module__', "dex_connectors.figma.figmamock")

def get_file_comments_invalid_key() -> Case[FileComments]:
    """Return the manifest invalidKey outcome."""
    return Case.branch_result(PROVIDER_REJECTED, FileComments(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getFileComments", "Figma rejected the token."))

setattr(get_file_comments_invalid_key, '__module__', "dex_connectors.figma.figmamock")


class Mock:
    """Script manifest outcomes for this connector."""

    def __init__(self, connection_name: str = "figma") -> None:
        self.get_file_script: Query[GetFileInput, File] = Query(GET_FILE_DEFINITION)
        self.get_file_script.default(get_file_default_answer())
        self.get_file_comments_script: Query[GetFileCommentsInput, FileComments] = Query(GET_FILE_COMMENTS_DEFINITION)
        self.get_file_comments_script.default(get_file_comments_default_answer())
        self._connection = new_connection_with_operations(
            get_file=self.get_file_script,
            get_file_comments=self.get_file_comments_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock connection for Dex Step registration."""
        return self._connection

setattr(Mock, '__module__', "dex_connectors.figma.figmamock")

__all__ = [
    "Mock",
    "get_file_found",
    "get_file_not_found",
    "get_file_provider_rejected",
    "get_file_invalid_response",
    "get_file_defect",
    "get_file_retry",
    "get_file_default_answer",
    "get_file_invalid_key",
    "get_file_comments_listed",
    "get_file_comments_not_found",
    "get_file_comments_provider_rejected",
    "get_file_comments_invalid_response",
    "get_file_comments_defect",
    "get_file_comments_retry",
    "get_file_comments_default_answer",
    "get_file_comments_invalid_key",
]
