# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GenerateTextInput requests one text answer. MaxOutputTokens defaults to 1024 when zero.
@dataclass(frozen=True, slots=True)
class GenerateTextInput:
    # Prompt is a nonempty user message of at most 64 KiB.
    prompt: str = ''
    # MaxOutputTokens limits generated tokens to 1 through 4096; zero uses 1024.
    max_output_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxOutputTokens', 'dex_json_omit_default': True})


setattr(GenerateTextInput, '__module__', 'dex_connectors.zhipu.models')


# TokenUsage reports provider token accounting.
@dataclass(frozen=True, slots=True)
class TokenUsage:
    # PromptTokens counts tokens in the request context.
    prompt_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'promptTokens'})
    # CompletionTokens counts generated tokens.
    completion_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'completionTokens'})
    # TotalTokens is the provider-reported sum.
    total_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalTokens'})


setattr(TokenUsage, '__module__', 'dex_connectors.zhipu.models')


# GenerateTextOutput holds a bounded text answer and its provider metadata.
@dataclass(frozen=True, slots=True)
class GenerateTextOutput:
    # Text is the answer content, possibly partial on the truncated branch.
    text: str = ''
    # Model is the model reported by Zhipu.
    model: str = ''
    # ResponseID is the non-secret provider completion ID.
    response_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'responseId'})
    # FinishReason is the provider's stop reason.
    finish_reason: str = _dataclass_field(default='', metadata={'dex_json_name': 'finishReason'})
    # Usage is the provider's reported token count.
    usage: TokenUsage = _dataclass_field(default_factory=TokenUsage)


setattr(GenerateTextOutput, '__module__', 'dex_connectors.zhipu.models')
