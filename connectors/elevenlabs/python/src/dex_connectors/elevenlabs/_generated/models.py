# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# ListVoicesInput selects one page of account voices.
@dataclass(frozen=True, slots=True)
class ListVoicesInput:
    # PageSize is the requested page size; zero uses the provider default.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # NextPageToken is a token returned by the preceding page; blank starts at page one.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})


setattr(ListVoicesInput, '__module__', 'dex_connectors.elevenlabs.models')


# Voice identifies an ElevenLabs voice.
@dataclass(frozen=True, slots=True)
class Voice:
    # VoiceID is the stable provider voice identifier.
    voice_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'voiceId'})
    # Name is the display name.
    name: str = ''


setattr(Voice, '__module__', 'dex_connectors.elevenlabs.models')


# ListVoicesOutput holds one voice page.
@dataclass(frozen=True, slots=True)
class ListVoicesOutput:
    # Voices is the page of available voices.
    voices: tuple[Voice, ...] | None = None
    # HasMore reports whether another page is available.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})
    # NextPageToken selects the next page when HasMore is true.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})


setattr(ListVoicesOutput, '__module__', 'dex_connectors.elevenlabs.models')


# CreateSpeechInput selects a voice and the text to synthesize.
@dataclass(frozen=True, slots=True)
class CreateSpeechInput:
    # VoiceID is returned by listVoices or shown in the ElevenLabs voice library.
    voice_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'voiceId'})
    # Text is the text to speak.
    text: str = ''
    # ModelID selects a text-to-speech model; blank uses eleven_multilingual_v2.
    model_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'modelId', 'dex_json_omit_default': True})


setattr(CreateSpeechInput, '__module__', 'dex_connectors.elevenlabs.models')


# CreateSpeechOutput holds bounded MP3 audio as base64.
@dataclass(frozen=True, slots=True)
class CreateSpeechOutput:
    # AudioBase64 is the MP3 payload encoded for Dex persistence.
    audio_base64: str = _dataclass_field(default='', metadata={'dex_json_name': 'audioBase64'})
    # ContentType identifies the MP3 format.
    content_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentType'})


setattr(CreateSpeechOutput, '__module__', 'dex_connectors.elevenlabs.models')
