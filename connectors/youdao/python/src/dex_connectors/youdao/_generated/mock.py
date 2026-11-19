# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import TextTranslation, TranslateTextInput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, PROVIDER_REJECTED, TRANSLATED, TRANSLATE_TEXT_DEFINITION

def translate_text_translated(value: TextTranslation, failure: Failure | None = None) -> Case[TextTranslation]:
    return Case.branch_result(TRANSLATED, value, failure)

setattr(translate_text_translated, '__module__', "dex_connectors.youdao.youdaomock")

def translate_text_provider_rejected(value: TextTranslation, failure: Failure | None = None) -> Case[TextTranslation]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(translate_text_provider_rejected, '__module__', "dex_connectors.youdao.youdaomock")

def translate_text_invalid_response(value: TextTranslation, failure: Failure | None = None) -> Case[TextTranslation]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(translate_text_invalid_response, '__module__', "dex_connectors.youdao.youdaomock")

def translate_text_defect(value: TextTranslation, failure: Failure | None = None) -> Case[TextTranslation]:
    return Case.branch_result(DEFECT, value, failure)

setattr(translate_text_defect, '__module__', "dex_connectors.youdao.youdaomock")

def translate_text_retry(failure: Failure, after_seconds: float = 0) -> Case[TextTranslation]:
    return Case.retry(failure, after_seconds)

setattr(translate_text_retry, '__module__', "dex_connectors.youdao.youdaomock")

def translate_text_translated_default() -> Case[TextTranslation]:
    return Case.branch_result(TRANSLATED, TextTranslation(translation=('你好',), source_language='en', target_language='zh-CHS'))

setattr(translate_text_translated_default, '__module__', "dex_connectors.youdao.youdaomock")


class Mock:

    def __init__(self, connection_name: str = "youdao") -> None:
        self.translate_text_script: Query[TranslateTextInput, TextTranslation] = Query(TRANSLATE_TEXT_DEFINITION)
        self.translate_text_script.default(translate_text_translated_default())
        self._connection = new_connection_with_operations(
            translate_text=self.translate_text_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.youdao.youdaomock")

__all__ = [
    "Mock",
    "translate_text_translated",
    "translate_text_provider_rejected",
    "translate_text_invalid_response",
    "translate_text_defect",
    "translate_text_retry",
    "translate_text_translated_default",
]
