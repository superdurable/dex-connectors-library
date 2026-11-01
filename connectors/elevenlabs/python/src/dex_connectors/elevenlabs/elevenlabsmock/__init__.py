# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Case as _Case
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import create_speech_created as create_speech_created
from .._generated.mock import create_speech_created_default as _create_speech_created_default
from .._generated.mock import list_voices_listed as list_voices_listed
from .._generated.mock import list_voices_listed_default as _list_voices_listed_default
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import CREATE_SPEECH_DEFINITION as CREATE_SPEECH_DEFINITION
from ..client import CREATED as CREATED
from ..client import LIST_VOICES_DEFINITION as LIST_VOICES_DEFINITION
from ..client import LISTED as LISTED
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import CreateSpeechInput as CreateSpeechInput
from ..models import CreateSpeechOutput as CreateSpeechOutput
from ..models import CreateSpeechOutput as _CreateSpeechOutput
from ..models import ListVoicesInput as ListVoicesInput
from ..models import ListVoicesOutput as ListVoicesOutput
from ..models import ListVoicesOutput as _ListVoicesOutput
from ..models import Voice as Voice


def create_speech_default_answer() -> _Case[_CreateSpeechOutput]:
    """Return the manifest's createSpeech default answer."""
    return _create_speech_created_default()


def list_voices_default_answer() -> _Case[_ListVoicesOutput]:
    """Return the manifest's listVoices default answer."""
    return _list_voices_listed_default()


__all__ = [
    "Mock",
    "list_voices_listed",
    "list_voices_default_answer",
    "create_speech_created",
    "create_speech_default_answer",
]

Mock.__doc__ = "Script ElevenLabs operations, defaulting to their manifest answers."

list_voices_listed.__doc__ = "Script the listed route."
create_speech_created.__doc__ = "Script the created route."
