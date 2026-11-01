# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""ElevenLabs voice listing and speech generation operations for Dex connector Steps."""

from .client import (
    CONNECTOR_ID,
    CREATE_SPEECH_DEFINITION,
    CREATED,
    DEFAULT_MODEL_ID,
    DEFECT,
    INVALID_RESPONSE,
    LIST_VOICES_DEFINITION,
    LISTED,
    PROVIDER_REJECTED,
    SPEECH_OUTPUT_FORMAT,
    Client,
    Config,
    CreateSpeechOperation,
    Credentials,
    ListVoicesOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import CreateSpeechInput, CreateSpeechOutput, ListVoicesInput, ListVoicesOutput, Voice
from .steps import new_create_speech_step, new_list_voices_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "CreateSpeechInput",
    "CreateSpeechOperation",
    "CreateSpeechOutput",
    "Credentials",
    "ListVoicesInput",
    "ListVoicesOperation",
    "ListVoicesOutput",
    "Voice",
    "new_connection",
    "new_connection_with_operations",
    "new_create_speech_step",
    "new_list_voices_step",
    "new_project_connection",
    "CONNECTOR_ID",
    "CREATE_SPEECH_DEFINITION",
    "CREATED",
    "DEFAULT_MODEL_ID",
    "DEFECT",
    "INVALID_RESPONSE",
    "LIST_VOICES_DEFINITION",
    "LISTED",
    "PROVIDER_REJECTED",
    "SPEECH_OUTPUT_FORMAT",
]
