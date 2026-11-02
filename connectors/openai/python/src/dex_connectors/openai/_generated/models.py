# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

# StructuredOutput configures a JSON Schema response format for CreateRequest.
@dataclass(frozen=True, slots=True)
class StructuredOutput:
    # Name is the schema name sent to OpenAI.
    name: str = ''
    # Description optionally explains the schema to the model.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Schema is the JSON Schema sent to OpenAI.
    schema: dict[str, Any] | None = None
    # Strict requests exact schema adherence when true.
    strict: bool = False


setattr(StructuredOutput, '__module__', 'dex_connectors.openai.models')


# CreateRequest contains the provider request fields for create.
@dataclass(frozen=True, slots=True)
class CreateRequest:
    # Model is the OpenAI model ID for this response. Empty uses the connection's model.
    model: str = _dataclass_field(default='', metadata={'dex_json_name': 'Model'})
    # Input specifies input for create request.
    input: Any = _dataclass_field(default=None, metadata={'dex_json_name': 'Input'})
    # Instructions specifies instructions for create request.
    instructions: str = _dataclass_field(default='', metadata={'dex_json_name': 'Instructions'})
    # StructuredOutput specifies structured output for create request.
    structured_output: StructuredOutput | None = _dataclass_field(default=None, metadata={'dex_json_name': 'StructuredOutput'})


setattr(CreateRequest, '__module__', 'dex_connectors.openai.models')


# Usage represents the connector's usage data.
@dataclass(frozen=True, slots=True)
class Usage:
    # InputTokens is the number of input tokens billed by the provider.
    input_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'inputTokens'})
    # CachedInputTokens is the cached subset of InputTokens.
    cached_input_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'cachedInputTokens'})
    # OutputTokens is the number of output tokens billed by the provider.
    output_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'outputTokens'})
    # ReasoningOutputTokens is the reasoning subset of OutputTokens.
    reasoning_output_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'reasoningOutputTokens'})
    # TotalTokens is the total number of billed tokens.
    total_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalTokens'})


setattr(Usage, '__module__', 'dex_connectors.openai.models')


# Response contains the normalized fields returned by the OpenAI Responses API.
@dataclass(frozen=True, slots=True)
class Response:
    # ID is the stable provider identifier.
    id: str = ''
    # Model is the model returned by OpenAI.
    model: str = ''
    # Status is the status returned by OpenAI.
    status: str = ''
    # OutputText is the output text returned by OpenAI.
    output_text: str = _dataclass_field(default='', metadata={'dex_json_name': 'outputText'})
    # Usage is the usage returned by OpenAI.
    usage: Usage = _dataclass_field(default_factory=Usage)


setattr(Response, '__module__', 'dex_connectors.openai.models')


# RetrieveRequest contains the provider request fields for retrieve.
@dataclass(frozen=True, slots=True)
class RetrieveRequest:
    # ResponseID is the OpenAI response identifier.
    response_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'ResponseID'})


setattr(RetrieveRequest, '__module__', 'dex_connectors.openai.models')
