# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    CreateSpeechInput as CreateSpeechInput,
)
from ._generated.models import (
    CreateSpeechOutput as CreateSpeechOutput,
)
from ._generated.models import (
    ListVoicesInput as ListVoicesInput,
)
from ._generated.models import ListVoicesOutput as ListVoicesOutput
from ._generated.models import (
    Voice as Voice,
)

ListVoicesOutput.__doc__ = "One voice page and the token that selects the next page when has_more is true."

ListVoicesInput.__doc__ = (
    "Page size from 0 (provider default) through 100 and the preceding page token (blank for page one)."
)
Voice.__doc__ = "A stable ElevenLabs voice ID and its display name."
CreateSpeechInput.__doc__ = (
    "Voice ID from listVoices, the text to speak, and a model ID; blank uses eleven_multilingual_v2."
)
CreateSpeechOutput.__doc__ = "Bounded MP3 audio encoded as base64 for Dex persistence, and its content type."
