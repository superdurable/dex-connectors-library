# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed ElevenLabs scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Query

from ..client import CONNECTOR_ID, CREATE_SPEECH_DEFINITION, CREATED, LIST_VOICES_DEFINITION, LISTED
from ..connection import Connection, new_connection_with_operations
from ..models import CreateSpeechInput, CreateSpeechOutput, ListVoicesInput, ListVoicesOutput, Voice


def list_voices_listed(value: ListVoicesOutput, failure: Failure | None = None) -> Case[ListVoicesOutput]:
    """Script the listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_voices_default_answer() -> Case[ListVoicesOutput]:
    """Return the manifest's listVoices default answer."""
    return list_voices_listed(ListVoicesOutput((Voice("voice_meridian", "Meridian Narrator"),), False))


def create_speech_created(
    value: CreateSpeechOutput, failure: Failure | None = None
) -> Case[CreateSpeechOutput]:
    """Script the created route."""
    return Case.branch_result(CREATED, value, failure)


def create_speech_default_answer() -> Case[CreateSpeechOutput]:
    """Return the manifest's createSpeech default answer."""
    return create_speech_created(CreateSpeechOutput("SUQz", "audio/mpeg"))


class Mock:
    """Script ElevenLabs operations, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "elevenlabs") -> None:
        self.list_voices_script: Query[ListVoicesInput, ListVoicesOutput] = Query(LIST_VOICES_DEFINITION)
        self.list_voices_script.default(list_voices_default_answer())
        self.create_speech_script: Query[CreateSpeechInput, CreateSpeechOutput] = Query(
            CREATE_SPEECH_DEFINITION
        )
        self.create_speech_script.default(create_speech_default_answer())
        self._connection = new_connection_with_operations(
            self.list_voices_script, self.create_speech_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "list_voices_listed",
    "list_voices_default_answer",
    "create_speech_created",
    "create_speech_default_answer",
]
