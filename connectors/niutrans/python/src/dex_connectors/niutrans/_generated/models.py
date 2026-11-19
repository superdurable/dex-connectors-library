# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# TranslateTextInput supplies bounded source text and language codes.
@dataclass(frozen=True, slots=True)
class TranslateTextInput:
    # Text is the source content, limited to 1000 Unicode characters.
    text: str = ''
    # From is the NiuTrans source language code, such as zh or auto.
    from_: str = _dataclass_field(default='', metadata={'dex_json_name': 'from'})
    # To is the NiuTrans target language code, such as en.
    to: str = ''


setattr(TranslateTextInput, '__module__', 'dex_connectors.niutrans.models')


# TranslateTextOutput contains only the translated text, not provider echoes or the API key.
@dataclass(frozen=True, slots=True)
class TranslateTextOutput:
    # TranslatedText is the returned translation.
    translated_text: str = _dataclass_field(default='', metadata={'dex_json_name': 'translatedText'})


setattr(TranslateTextOutput, '__module__', 'dex_connectors.niutrans.models')
