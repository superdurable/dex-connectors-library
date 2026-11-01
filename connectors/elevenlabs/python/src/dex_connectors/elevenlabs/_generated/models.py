# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class ListVoicesInput:
    """Typed ListVoicesInput value from the Go connector contract."""

    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})


setattr(ListVoicesInput, '__module__', 'dex_connectors.elevenlabs.models')


@dataclass(frozen=True, slots=True)
class Voice:
    """Typed Voice value from the Go connector contract."""

    voice_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'voiceId'})
    name: str = ''


setattr(Voice, '__module__', 'dex_connectors.elevenlabs.models')


@dataclass(frozen=True, slots=True)
class ListVoicesOutput:
    """Typed ListVoicesOutput value from the Go connector contract."""

    voices: tuple[Voice, ...] | None = None
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})


setattr(ListVoicesOutput, '__module__', 'dex_connectors.elevenlabs.models')


@dataclass(frozen=True, slots=True)
class CreateSpeechInput:
    """Typed CreateSpeechInput value from the Go connector contract."""

    voice_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'voiceId'})
    text: str = ''
    model_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'modelId', 'dex_json_omit_default': True})


setattr(CreateSpeechInput, '__module__', 'dex_connectors.elevenlabs.models')


@dataclass(frozen=True, slots=True)
class CreateSpeechOutput:
    """Typed CreateSpeechOutput value from the Go connector contract."""

    audio_base64: str = _dataclass_field(default='', metadata={'dex_json_name': 'audioBase64'})
    content_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentType'})


setattr(CreateSpeechOutput, '__module__', 'dex_connectors.elevenlabs.models')
