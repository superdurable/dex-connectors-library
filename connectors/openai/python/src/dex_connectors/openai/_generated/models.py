# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

@dataclass(frozen=True, slots=True)
class StructuredOutput:
    """Typed StructuredOutput value from the Go connector contract."""

    name: str = ''
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    schema: dict[str, Any] | None = None
    strict: bool = False


setattr(StructuredOutput, '__module__', 'dex_connectors.openai.models')


@dataclass(frozen=True, slots=True)
class CreateRequest:
    """Typed CreateRequest value from the Go connector contract."""

    model: str = _dataclass_field(default='', metadata={'dex_json_name': 'Model'})
    input: Any = _dataclass_field(default=None, metadata={'dex_json_name': 'Input'})
    instructions: str = _dataclass_field(default='', metadata={'dex_json_name': 'Instructions'})
    structured_output: StructuredOutput | None = _dataclass_field(default=None, metadata={'dex_json_name': 'StructuredOutput'})


setattr(CreateRequest, '__module__', 'dex_connectors.openai.models')


@dataclass(frozen=True, slots=True)
class Usage:
    """Typed Usage value from the Go connector contract."""

    input_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'inputTokens'})
    cached_input_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'cachedInputTokens'})
    output_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'outputTokens'})
    reasoning_output_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'reasoningOutputTokens'})
    total_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalTokens'})


setattr(Usage, '__module__', 'dex_connectors.openai.models')


@dataclass(frozen=True, slots=True)
class Response:
    """Typed Response value from the Go connector contract."""

    id: str = ''
    model: str = ''
    status: str = ''
    output_text: str = _dataclass_field(default='', metadata={'dex_json_name': 'outputText'})
    usage: Usage = _dataclass_field(default_factory=Usage)


setattr(Response, '__module__', 'dex_connectors.openai.models')


@dataclass(frozen=True, slots=True)
class RetrieveRequest:
    """Typed RetrieveRequest value from the Go connector contract."""

    response_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'ResponseID'})


setattr(RetrieveRequest, '__module__', 'dex_connectors.openai.models')
