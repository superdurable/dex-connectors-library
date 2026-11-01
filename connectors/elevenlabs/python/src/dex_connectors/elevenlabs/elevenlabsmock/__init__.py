# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from .._generated.mock import Mock as Mock
from .._generated.mock import create_speech_created as create_speech_created
from .._generated.mock import create_speech_created_default as create_speech_default_answer
from .._generated.mock import list_voices_listed as list_voices_listed
from .._generated.mock import list_voices_listed_default as list_voices_default_answer

__all__ = [
    'Mock',
    'list_voices_listed',
    'list_voices_default_answer',
    'create_speech_created',
    'create_speech_default_answer',
]
