# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListSystemVoicesInput requests the predefined voice catalog without parameters.
@dataclass(frozen=True, slots=True)
class ListSystemVoicesInput:
    pass


setattr(ListSystemVoicesInput, '__module__', 'dex_connectors.unisound.models')


# SystemVoice describes one predefined text-to-speech voice.
@dataclass(frozen=True, slots=True)
class SystemVoice:
    # VoiceID is the identifier accepted by Unisound text-to-speech APIs.
    voice_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'voiceID'})
    # Name is the displayed Chinese voice name.
    name: str = ''
    # Languages are the voice's language codes.
    languages: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'languages', _replace_nil_slices(self.languages, 1))


SystemVoice.__annotations__['languages'] = tuple[str, ...] | None
SystemVoice.__dataclass_fields__['languages'].type = tuple[str, ...] | None
setattr(SystemVoice, '__module__', 'dex_connectors.unisound.models')


# ListSystemVoicesOutput contains up to 25 predefined voices.
@dataclass(frozen=True, slots=True)
class ListSystemVoicesOutput:
    # Voices are provider-ordered system voices.
    voices: tuple[SystemVoice, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'voices', _replace_nil_slices(self.voices, 1))


ListSystemVoicesOutput.__annotations__['voices'] = tuple[SystemVoice, ...] | None
ListSystemVoicesOutput.__dataclass_fields__['voices'].type = tuple[SystemVoice, ...] | None
setattr(ListSystemVoicesOutput, '__module__', 'dex_connectors.unisound.models')
