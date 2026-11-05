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


@dataclass(frozen=True, slots=True)
class textgenMessage:
    role: str = ''
    text: str = ''


setattr(textgenMessage, '__module__', 'dex_connectors.superdurable_llm.models')


@dataclass(frozen=True, slots=True)
class textgenStructuredOutput:
    name: str = ''
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    schema: dict[str, Any] | None = None


setattr(textgenStructuredOutput, '__module__', 'dex_connectors.superdurable_llm.models')


# GenerateTextRequest is the provider-neutral generateText input. Its Model is
# the provider's own model ID, or blank for the connection's model.
@dataclass(frozen=True, slots=True)
class GenerateTextRequest:
    model: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    instructions: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    messages: tuple[textgenMessage, ...] = ()
    structured_output: textgenStructuredOutput | None = _dataclass_field(default=None, metadata={'dex_json_name': 'structuredOutput', 'dex_json_omit_default': True})
    max_output_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxOutputTokens', 'dex_json_omit_default': True})
    temperature: float | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    reasoning_effort: str = _dataclass_field(default='', metadata={'dex_json_name': 'reasoningEffort', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'messages', _replace_nil_slices(self.messages, 1))


GenerateTextRequest.__annotations__['messages'] = tuple[textgenMessage, ...] | None
GenerateTextRequest.__dataclass_fields__['messages'].type = tuple[textgenMessage, ...] | None
setattr(GenerateTextRequest, '__module__', 'dex_connectors.superdurable_llm.models')


@dataclass(frozen=True, slots=True)
class textgenUsage:
    input_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'inputTokens'})
    cached_input_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'cachedInputTokens', 'dex_json_omit_default': True})
    output_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'outputTokens'})
    reasoning_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'reasoningTokens', 'dex_json_omit_default': True})
    total_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalTokens'})


setattr(textgenUsage, '__module__', 'dex_connectors.superdurable_llm.models')


# GenerateTextResponse is the provider-neutral generateText output. Its
# RequestedModel is the provider's model ID that the request was sent for.
@dataclass(frozen=True, slots=True)
class GenerateTextResponse:
    text: str = ''
    requested_model: str = _dataclass_field(default='', metadata={'dex_json_name': 'requestedModel', 'dex_json_omit_default': True})
    served_model: str = _dataclass_field(default='', metadata={'dex_json_name': 'servedModel', 'dex_json_omit_default': True})
    response_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'responseId', 'dex_json_omit_default': True})
    finish_reason: str = _dataclass_field(default='', metadata={'dex_json_name': 'finishReason', 'dex_json_omit_default': True})
    provider_finish_reason: str = _dataclass_field(default='', metadata={'dex_json_name': 'providerFinishReason', 'dex_json_omit_default': True})
    usage: textgenUsage = _dataclass_field(default_factory=textgenUsage)


setattr(GenerateTextResponse, '__module__', 'dex_connectors.superdurable_llm.models')
