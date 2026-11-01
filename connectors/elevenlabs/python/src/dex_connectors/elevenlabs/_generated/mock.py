# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated manifest-backed provider outcomes and scripts."""

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CreateSpeechInput, CreateSpeechOutput, ListVoicesInput, ListVoicesOutput, Voice
from .definitions import CONNECTOR_ID, CREATED, CREATE_SPEECH_DEFINITION, DEFECT, INVALID_RESPONSE, LISTED, LIST_VOICES_DEFINITION, PROVIDER_REJECTED

def list_voices_listed(value: ListVoicesOutput, failure: Failure | None = None) -> Case[ListVoicesOutput]:
    """Script the listed branch."""
    return Case.branch_result(LISTED, value, failure)

setattr(list_voices_listed, '__module__', "dex_connectors.elevenlabs.elevenlabsmock")

def list_voices_provider_rejected(value: ListVoicesOutput, failure: Failure | None = None) -> Case[ListVoicesOutput]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_voices_provider_rejected, '__module__', "dex_connectors.elevenlabs.elevenlabsmock")

def list_voices_invalid_response(value: ListVoicesOutput, failure: Failure | None = None) -> Case[ListVoicesOutput]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_voices_invalid_response, '__module__', "dex_connectors.elevenlabs.elevenlabsmock")

def list_voices_defect(value: ListVoicesOutput, failure: Failure | None = None) -> Case[ListVoicesOutput]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(list_voices_defect, '__module__', "dex_connectors.elevenlabs.elevenlabsmock")

def list_voices_retry(failure: Failure, after_seconds: float = 0) -> Case[ListVoicesOutput]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(list_voices_retry, '__module__', "dex_connectors.elevenlabs.elevenlabsmock")

def list_voices_listed_default() -> Case[ListVoicesOutput]:
    """Return the manifest listedDefault outcome."""
    return Case.branch_result(LISTED, ListVoicesOutput(voices=(Voice(voice_id='voice_meridian', name='Meridian Narrator'),), has_more=False))

setattr(list_voices_listed_default, '__module__', "dex_connectors.elevenlabs.elevenlabsmock")

def create_speech_created(value: CreateSpeechOutput, failure: Failure | None = None) -> Case[CreateSpeechOutput]:
    """Script the created branch."""
    return Case.branch_result(CREATED, value, failure)

setattr(create_speech_created, '__module__', "dex_connectors.elevenlabs.elevenlabsmock")

def create_speech_provider_rejected(value: CreateSpeechOutput, failure: Failure | None = None) -> Case[CreateSpeechOutput]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_speech_provider_rejected, '__module__', "dex_connectors.elevenlabs.elevenlabsmock")

def create_speech_invalid_response(value: CreateSpeechOutput, failure: Failure | None = None) -> Case[CreateSpeechOutput]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(create_speech_invalid_response, '__module__', "dex_connectors.elevenlabs.elevenlabsmock")

def create_speech_defect(value: CreateSpeechOutput, failure: Failure | None = None) -> Case[CreateSpeechOutput]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(create_speech_defect, '__module__', "dex_connectors.elevenlabs.elevenlabsmock")

def create_speech_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateSpeechOutput]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(create_speech_retry, '__module__', "dex_connectors.elevenlabs.elevenlabsmock")

def create_speech_created_default() -> Case[CreateSpeechOutput]:
    """Return the manifest createdDefault outcome."""
    return Case.branch_result(CREATED, CreateSpeechOutput(audio_base64='SUQz', content_type='audio/mpeg'))

setattr(create_speech_created_default, '__module__', "dex_connectors.elevenlabs.elevenlabsmock")


class Mock:
    """Script manifest outcomes for this connector."""

    def __init__(self, connection_name: str = "elevenlabs") -> None:
        self.list_voices_script: Query[ListVoicesInput, ListVoicesOutput] = Query(LIST_VOICES_DEFINITION)
        self.list_voices_script.default(list_voices_listed_default())
        self.create_speech_script: Query[CreateSpeechInput, CreateSpeechOutput] = Query(CREATE_SPEECH_DEFINITION)
        self.create_speech_script.default(create_speech_created_default())
        self._connection = new_connection_with_operations(
            list_voices=self.list_voices_script,
            create_speech=self.create_speech_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock connection for Dex Step registration."""
        return self._connection

setattr(Mock, '__module__', "dex_connectors.elevenlabs.elevenlabsmock")

__all__ = [
    "Mock",
    "list_voices_listed",
    "list_voices_provider_rejected",
    "list_voices_invalid_response",
    "list_voices_defect",
    "list_voices_retry",
    "list_voices_listed_default",
    "create_speech_created",
    "create_speech_provider_rejected",
    "create_speech_invalid_response",
    "create_speech_defect",
    "create_speech_retry",
    "create_speech_created_default",
]
