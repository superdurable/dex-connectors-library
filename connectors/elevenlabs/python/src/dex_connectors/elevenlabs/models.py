# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass, field

from ._generated.models import (
    CreateSpeechInput as CreateSpeechInput,
)
from ._generated.models import (
    CreateSpeechOutput as CreateSpeechOutput,
)
from ._generated.models import (
    ListVoicesInput as ListVoicesInput,
)
from ._generated.models import (
    Voice as Voice,
)


@dataclass(frozen=True, slots=True)
class ListVoicesOutput:
    """One voice page and the token that selects the next page when has_more is true."""

    voices: tuple[Voice, ...] = ()
    has_more: bool = field(default=False, metadata={"dex_json_name": "hasMore"})
    next_page_token: str = field(
        default="", metadata={"dex_json_name": "nextPageToken", "dex_json_omit_default": True}
    )
