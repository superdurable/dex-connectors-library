# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import TranslateTextInput, TranslateTextOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, PROVIDER_REJECTED, TRANSLATED, TRANSLATE_TEXT_DEFINITION

def translate_text_translated(value: TranslateTextOutput, failure: Failure | None = None) -> Case[TranslateTextOutput]:
    return Case.branch_result(TRANSLATED, value, failure)

setattr(translate_text_translated, '__module__', "dex_connectors.niutrans.niutransmock")

def translate_text_provider_rejected(value: TranslateTextOutput, failure: Failure | None = None) -> Case[TranslateTextOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(translate_text_provider_rejected, '__module__', "dex_connectors.niutrans.niutransmock")

def translate_text_invalid_response(value: TranslateTextOutput, failure: Failure | None = None) -> Case[TranslateTextOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(translate_text_invalid_response, '__module__', "dex_connectors.niutrans.niutransmock")

def translate_text_defect(value: TranslateTextOutput, failure: Failure | None = None) -> Case[TranslateTextOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(translate_text_defect, '__module__', "dex_connectors.niutrans.niutransmock")

def translate_text_retry(failure: Failure, after_seconds: float = 0) -> Case[TranslateTextOutput]:
    return Case.retry(failure, after_seconds)

setattr(translate_text_retry, '__module__', "dex_connectors.niutrans.niutransmock")

def translate_text_sample_translation() -> Case[TranslateTextOutput]:
    return Case.branch_result(TRANSLATED, TranslateTextOutput(translated_text='Hello.'))

setattr(translate_text_sample_translation, '__module__', "dex_connectors.niutrans.niutransmock")


class Mock:

    def __init__(self, connection_name: str = "niutrans") -> None:
        self.translate_text_script: Query[TranslateTextInput, TranslateTextOutput] = Query(TRANSLATE_TEXT_DEFINITION)
        self.translate_text_script.default(translate_text_sample_translation())
        self._connection = new_connection_with_operations(
            translate_text=self.translate_text_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.niutrans.niutransmock")

__all__ = [
    "Mock",
    "translate_text_translated",
    "translate_text_provider_rejected",
    "translate_text_invalid_response",
    "translate_text_defect",
    "translate_text_retry",
    "translate_text_sample_translation",
]
