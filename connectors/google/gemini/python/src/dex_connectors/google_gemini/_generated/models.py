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


# Part is one text part of a Content.
@dataclass(frozen=True, slots=True)
class Part:
    # Text is the part text. It must not be empty.
    text: str = ''


setattr(Part, '__module__', 'dex_connectors.google_gemini.models')


# Content is one conversation turn. Role is "user", "model", or empty for a
# single-turn request.
@dataclass(frozen=True, slots=True)
class Content:
    # Role is "user", "model", or empty for a single-turn request.
    role: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Parts holds the turn's text parts in order. At least one part is required.
    parts: tuple[Part, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'parts', _replace_nil_slices(self.parts, 1))


Content.__annotations__['parts'] = tuple[Part, ...] | None
Content.__dataclass_fields__['parts'].type = tuple[Part, ...] | None
setattr(Content, '__module__', 'dex_connectors.google_gemini.models')


# GenerateContentRequest is the provider input for GenerateContent.
# 
# Model accepts a model ID such as "gemini-3.5-flash-lite" or
# "models/gemini-3.5-flash-lite"; empty uses the connection's configured model.
# ResponseJSONSchema maps to generationConfig.responseJsonSchema and defaults
# ResponseMIMEType to application/json. ThinkingBudget maps to
# generationConfig.thinkingConfig.thinkingBudget: nil leaves the model default,
# -1 selects dynamic thinking, 0 disables thinking where the model allows it,
# and a positive value sets a token budget. Gemini 3 models treat
# thinkingBudget as a legacy setting that may not turn thinking off, and Google
# recommends their default temperature of 1.0, so leave ThinkingBudget and
# Temperature nil for them.
@dataclass(frozen=True, slots=True)
class GenerateContentRequest:
    # Model is the Gemini model ID, with or without the "models/" prefix. It
    # overrides the connection's configured model for this request; empty uses
    # the connection model.
    model: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # SystemInstruction is optional system text sent as systemInstruction.parts[0].text.
    system_instruction: str = _dataclass_field(default='', metadata={'dex_json_name': 'systemInstruction', 'dex_json_omit_default': True})
    # Contents holds the conversation turns in order. At least one Content is required.
    contents: tuple[Content, ...] = ()
    # ResponseMIMEType maps to generationConfig.responseMimeType. Empty omits it
    # unless ResponseJSONSchema is set.
    response_mime_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'responseMimeType', 'dex_json_omit_default': True})
    # ResponseJSONSchema maps to generationConfig.responseJsonSchema and requires
    # the application/json response MIME type. Nil omits it.
    response_json_schema: dict[str, Any] = _dataclass_field(default_factory=dict, metadata={'dex_json_name': 'responseJsonSchema', 'dex_json_omit_default': True})
    # Temperature maps to generationConfig.temperature and must be between 0 and 2.
    # Nil leaves the model default.
    temperature: float | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # MaxOutputTokens maps to generationConfig.maxOutputTokens and includes thought
    # tokens. Zero leaves the model default; a negative value is invalid.
    max_output_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxOutputTokens', 'dex_json_omit_default': True})
    # ThinkingBudget maps to generationConfig.thinkingConfig.thinkingBudget. Nil
    # leaves the model default, -1 is dynamic, 0 disables thinking where the model
    # allows it, and a positive value is a token budget.
    thinking_budget: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'thinkingBudget', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'contents', _replace_nil_slices(self.contents, 1))


GenerateContentRequest.__annotations__['contents'] = tuple[Content, ...] | None
GenerateContentRequest.__dataclass_fields__['contents'].type = tuple[Content, ...] | None
setattr(GenerateContentRequest, '__module__', 'dex_connectors.google_gemini.models')


# Usage is the token usage Gemini reported for one request.
@dataclass(frozen=True, slots=True)
class Usage:
    # PromptTokens is usageMetadata.promptTokenCount.
    prompt_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'promptTokens'})
    # CandidateTokens is usageMetadata.candidatesTokenCount.
    candidate_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'candidateTokens'})
    # ThoughtsTokens is usageMetadata.thoughtsTokenCount.
    thoughts_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'thoughtsTokens'})
    # CachedContentTokens is usageMetadata.cachedContentTokenCount, the cached subset of PromptTokens.
    cached_content_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'cachedContentTokens'})
    # TotalTokens is usageMetadata.totalTokenCount.
    total_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalTokens'})


setattr(Usage, '__module__', 'dex_connectors.google_gemini.models')


# GenerateContentResponse is the bounded result of one generateContent call.
# 
# Text concatenates the non-thought text parts of the first candidate. Only the
# generated and truncated branches carry Text; blocked and invalidResponse
# results keep identity, finish or block reason, and usage without content.
@dataclass(frozen=True, slots=True)
class GenerateContentResponse:
    # ResponseID is the Gemini responseId, also recorded as the Receipt provider object ID.
    response_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'responseId', 'dex_json_omit_default': True})
    # ModelVersion is the Gemini modelVersion that produced the response.
    model_version: str = _dataclass_field(default='', metadata={'dex_json_name': 'modelVersion', 'dex_json_omit_default': True})
    # Text is the first candidate's non-thought text. It is empty outside the
    # generated and truncated branches.
    text: str = ''
    # FinishReason is the first candidate's finish reason, such as STOP or MAX_TOKENS.
    finish_reason: str = _dataclass_field(default='', metadata={'dex_json_name': 'finishReason', 'dex_json_omit_default': True})
    # BlockReason is promptFeedback.blockReason when Gemini blocked the prompt.
    block_reason: str = _dataclass_field(default='', metadata={'dex_json_name': 'blockReason', 'dex_json_omit_default': True})
    # Usage is the token usage Gemini reported.
    usage: Usage = _dataclass_field(default_factory=Usage)


setattr(GenerateContentResponse, '__module__', 'dex_connectors.google_gemini.models')
