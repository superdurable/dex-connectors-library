# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetStoryInput, StoryMetadata
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_STORY_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_story_found(value: StoryMetadata, failure: Failure | None = None) -> Case[StoryMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_story_found, '__module__', "dex_connectors.shortcut.shortcutmock")

def get_story_not_found(value: StoryMetadata, failure: Failure | None = None) -> Case[StoryMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_story_not_found, '__module__', "dex_connectors.shortcut.shortcutmock")

def get_story_provider_rejected(value: StoryMetadata, failure: Failure | None = None) -> Case[StoryMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_story_provider_rejected, '__module__', "dex_connectors.shortcut.shortcutmock")

def get_story_invalid_response(value: StoryMetadata, failure: Failure | None = None) -> Case[StoryMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_story_invalid_response, '__module__', "dex_connectors.shortcut.shortcutmock")

def get_story_defect(value: StoryMetadata, failure: Failure | None = None) -> Case[StoryMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_story_defect, '__module__', "dex_connectors.shortcut.shortcutmock")

def get_story_retry(failure: Failure, after_seconds: float = 0) -> Case[StoryMetadata]:
    return Case.retry(failure, after_seconds)

setattr(get_story_retry, '__module__', "dex_connectors.shortcut.shortcutmock")

def get_story_found_default() -> Case[StoryMetadata]:
    return Case.branch_result(FOUND, StoryMetadata(story_id='12345', name='Example story', story_type='feature', workflow_state_id='678', archived=False))

setattr(get_story_found_default, '__module__', "dex_connectors.shortcut.shortcutmock")

def get_story_story_missing() -> Case[StoryMetadata]:
    return Case.branch_result(NOT_FOUND, StoryMetadata(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getStory", "Shortcut did not find the story."))

setattr(get_story_story_missing, '__module__', "dex_connectors.shortcut.shortcutmock")


class Mock:

    def __init__(self, connection_name: str = "shortcut") -> None:
        self.get_story_script: Query[GetStoryInput, StoryMetadata] = Query(GET_STORY_DEFINITION)
        self.get_story_script.default(get_story_found_default())
        self._connection = new_connection_with_operations(
            get_story=self.get_story_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.shortcut.shortcutmock")

__all__ = [
    "Mock",
    "get_story_found",
    "get_story_not_found",
    "get_story_provider_rejected",
    "get_story_invalid_response",
    "get_story_defect",
    "get_story_retry",
    "get_story_found_default",
    "get_story_story_missing",
]
