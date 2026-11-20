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


# TranslateTextInput gives the bounded source text and language codes.
@dataclass(frozen=True, slots=True)
class TranslateTextInput:
    # Text is UTF-8 source text of at most 5000 characters.
    text: str = ''
    # SourceLanguage is a Youdao language code or auto.
    source_language: str = _dataclass_field(default='', metadata={'dex_json_name': 'sourceLanguage'})
    # TargetLanguage is a Youdao target language code.
    target_language: str = _dataclass_field(default='', metadata={'dex_json_name': 'targetLanguage'})


setattr(TranslateTextInput, '__module__', 'dex_connectors.youdao.models')


# TextTranslation contains only the returned translations and language direction.
@dataclass(frozen=True, slots=True)
class TextTranslation:
    # Translation holds one or more translated strings.
    translation: tuple[str, ...] = ()
    # SourceLanguage is the requested source language code.
    source_language: str = _dataclass_field(default='', metadata={'dex_json_name': 'sourceLanguage'})
    # TargetLanguage is the requested target language code.
    target_language: str = _dataclass_field(default='', metadata={'dex_json_name': 'targetLanguage'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'translation', _replace_nil_slices(self.translation, 1))


TextTranslation.__annotations__['translation'] = tuple[str, ...] | None
TextTranslation.__dataclass_fields__['translation'].type = tuple[str, ...] | None
setattr(TextTranslation, '__module__', 'dex_connectors.youdao.models')
