# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Gemini generateContent requests, responses, and token usage."""

from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True, slots=True)
class Part:
    """One nonempty text part."""

    text: str = ""


@dataclass(frozen=True, slots=True)
class Content:
    """One conversation turn; role is user, model, or blank, and parts must not be empty."""

    role: str = field(default="", metadata={"dex_json_omit_default": True})
    parts: tuple[Part, ...] = ()


@dataclass(frozen=True, slots=True)
class GenerateContentRequest:
    """One stateless models.generateContent request.

    A blank model uses the connection's model. A response JSON Schema implies the
    application/json MIME type. Temperature ranges from 0 through 2; zero
    maxOutputTokens uses Gemini's limit; thinkingBudget is -1 (dynamic), 0 (off), or a
    positive token budget.
    """

    model: str = field(default="", metadata={"dex_json_omit_default": True})
    system_instruction: str = field(
        default="", metadata={"dex_json_name": "systemInstruction", "dex_json_omit_default": True}
    )
    contents: tuple[Content, ...] = ()
    response_mime_type: str = field(
        default="", metadata={"dex_json_name": "responseMimeType", "dex_json_omit_default": True}
    )
    response_json_schema: dict[str, Any] | None = field(
        default=None, metadata={"dex_json_name": "responseJsonSchema", "dex_json_omit_default": True}
    )
    temperature: float | None = field(default=None, metadata={"dex_json_omit_default": True})
    max_output_tokens: int = field(
        default=0, metadata={"dex_json_name": "maxOutputTokens", "dex_json_omit_default": True}
    )
    thinking_budget: int | None = field(
        default=None, metadata={"dex_json_name": "thinkingBudget", "dex_json_omit_default": True}
    )


@dataclass(frozen=True, slots=True)
class Usage:
    """Token counts Gemini reported for one generation."""

    prompt_tokens: int = field(default=0, metadata={"dex_json_name": "promptTokens"})
    candidate_tokens: int = field(default=0, metadata={"dex_json_name": "candidateTokens"})
    thoughts_tokens: int = field(default=0, metadata={"dex_json_name": "thoughtsTokens"})
    cached_content_tokens: int = field(default=0, metadata={"dex_json_name": "cachedContentTokens"})
    total_tokens: int = field(default=0, metadata={"dex_json_name": "totalTokens"})


@dataclass(frozen=True, slots=True)
class GenerateContentResponse:
    """The first candidate's non-thought text, its finish or block reason, model version, and usage."""

    response_id: str = field(
        default="", metadata={"dex_json_name": "responseId", "dex_json_omit_default": True}
    )
    model_version: str = field(
        default="", metadata={"dex_json_name": "modelVersion", "dex_json_omit_default": True}
    )
    text: str = ""
    finish_reason: str = field(
        default="", metadata={"dex_json_name": "finishReason", "dex_json_omit_default": True}
    )
    block_reason: str = field(
        default="", metadata={"dex_json_name": "blockReason", "dex_json_omit_default": True}
    )
    usage: Usage = field(default_factory=Usage)
