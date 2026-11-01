# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed ElevenLabs voice pages and speech generation."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class ListVoicesInput:
    """Page size from 0 (provider default) through 100 and the preceding page token (blank for page one)."""

    page_size: int = field(default=0, metadata={"dex_json_name": "pageSize", "dex_json_omit_default": True})
    next_page_token: str = field(
        default="", metadata={"dex_json_name": "nextPageToken", "dex_json_omit_default": True}
    )


@dataclass(frozen=True, slots=True)
class Voice:
    """A stable ElevenLabs voice ID and its display name."""

    voice_id: str = field(default="", metadata={"dex_json_name": "voiceId"})
    name: str = ""


@dataclass(frozen=True, slots=True)
class ListVoicesOutput:
    """One voice page and the token that selects the next page when has_more is true."""

    voices: tuple[Voice, ...] = ()
    has_more: bool = field(default=False, metadata={"dex_json_name": "hasMore"})
    next_page_token: str = field(
        default="", metadata={"dex_json_name": "nextPageToken", "dex_json_omit_default": True}
    )


@dataclass(frozen=True, slots=True)
class CreateSpeechInput:
    """Voice ID from listVoices, the text to speak, and a model ID; blank uses eleven_multilingual_v2."""

    voice_id: str = field(default="", metadata={"dex_json_name": "voiceId"})
    text: str = ""
    model_id: str = field(default="", metadata={"dex_json_name": "modelId", "dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class CreateSpeechOutput:
    """Bounded MP3 audio encoded as base64 for Dex persistence, and its content type."""

    audio_base64: str = field(default="", metadata={"dex_json_name": "audioBase64"})
    content_type: str = field(default="", metadata={"dex_json_name": "contentType"})
