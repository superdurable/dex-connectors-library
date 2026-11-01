# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed OpenAI Responses requests, stored responses, and token usage."""

from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True, slots=True)
class StructuredOutput:
    """JSON Schema that constrains the response text; strict asks OpenAI to enforce it exactly."""

    name: str
    schema: dict[str, Any]
    description: str = field(default="", metadata={"dex_json_omit_default": True})
    strict: bool = False


@dataclass(frozen=True, slots=True)
class CreateRequest:
    """One Responses API create request.

    input is a string or a JSON-serializable list of Responses input items. A
    blank model uses the connection's model.
    """

    input: Any = field(default=None, metadata={"dex_json_name": "Input"})
    model: str = field(default="", metadata={"dex_json_name": "Model"})
    instructions: str = field(default="", metadata={"dex_json_name": "Instructions"})
    structured_output: StructuredOutput | None = field(
        default=None, metadata={"dex_json_name": "StructuredOutput"}
    )


@dataclass(frozen=True, slots=True)
class RetrieveRequest:
    """Provider response ID returned by createResponse, such as resp_..."""

    response_id: str = field(default="", metadata={"dex_json_name": "ResponseID"})


@dataclass(frozen=True, slots=True)
class Usage:
    """Token counts reported by OpenAI for one response."""

    input_tokens: int = field(default=0, metadata={"dex_json_name": "inputTokens"})
    cached_input_tokens: int = field(default=0, metadata={"dex_json_name": "cachedInputTokens"})
    output_tokens: int = field(default=0, metadata={"dex_json_name": "outputTokens"})
    reasoning_output_tokens: int = field(default=0, metadata={"dex_json_name": "reasoningOutputTokens"})
    total_tokens: int = field(default=0, metadata={"dex_json_name": "totalTokens"})


@dataclass(frozen=True, slots=True)
class Response:
    """Stored OpenAI response; output_text joins every output_text content part."""

    id: str = ""
    model: str = ""
    status: str = ""
    output_text: str = field(default="", metadata={"dex_json_name": "outputText"})
    usage: Usage = field(default_factory=Usage)
