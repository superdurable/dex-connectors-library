# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Figma scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..client import (
    CONNECTOR_ID,
    FOUND,
    GET_FILE_COMMENTS_DEFINITION,
    GET_FILE_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
)
from ..connection import Connection, new_connection_with_operations
from ..models import File, FileComment, FileComments, GetFileCommentsInput, GetFileInput


def get_file_found(value: File, failure: Failure | None = None) -> Case[File]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_file_not_found(value: File, failure: Failure | None = None) -> Case[File]:
    """Script a missing file."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_file_default_answer() -> Case[File]:
    """Return the manifest's getFile defaultAnswer."""
    return get_file_found(
        File("Meridian Design", "2026-10-28T12:00:00Z", {"id": "0:0", "type": "DOCUMENT", "name": "Root"})
    )


def get_file_invalid_key() -> Case[File]:
    """Return the manifest's getFile invalidKey answer."""
    return Case.branch_result(
        PROVIDER_REJECTED,
        File(),
        Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getFile", "Figma rejected the token."),
    )


def get_file_comments_listed(value: FileComments, failure: Failure | None = None) -> Case[FileComments]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def get_file_comments_default_answer() -> Case[FileComments]:
    """Return the manifest's getFileComments defaultAnswer."""
    return get_file_comments_listed(FileComments((FileComment("1", "Review the header."),)))


def get_file_comments_invalid_key() -> Case[FileComments]:
    """Return the manifest's getFileComments invalidKey answer."""
    return Case.branch_result(
        PROVIDER_REJECTED,
        FileComments(),
        Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getFileComments", "Figma rejected the token."),
    )


class Mock:
    """Script Figma Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "figma") -> None:
        self.get_file_script: Query[GetFileInput, File] = Query(GET_FILE_DEFINITION)
        self.get_file_script.default(get_file_default_answer())
        self.get_file_comments_script: Query[GetFileCommentsInput, FileComments] = Query(
            GET_FILE_COMMENTS_DEFINITION
        )
        self.get_file_comments_script.default(get_file_comments_default_answer())
        self._connection = new_connection_with_operations(
            self.get_file_script, self.get_file_comments_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "get_file_found",
    "get_file_not_found",
    "get_file_default_answer",
    "get_file_invalid_key",
    "get_file_comments_listed",
    "get_file_comments_default_answer",
    "get_file_comments_invalid_key",
]
