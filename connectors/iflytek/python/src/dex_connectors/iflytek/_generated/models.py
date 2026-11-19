# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# CreateChatCompletionInput contains one bounded user prompt.
@dataclass(frozen=True, slots=True)
class CreateChatCompletionInput:
    # Prompt is nonempty UTF-8 text of at most 2000 characters.
    prompt: str = ''


setattr(CreateChatCompletionInput, '__module__', 'dex_connectors.iflytek.models')


# ChatCompletion contains one assistant answer and token usage.
@dataclass(frozen=True, slots=True)
class ChatCompletion:
    # Content is the assistant's nonempty text answer.
    content: str = ''
    # Model is the model ID selected by the connection.
    model: str = ''
    # PromptTokens is the number of billed input tokens reported by Spark.
    prompt_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'promptTokens'})
    # CompletionTokens is the number of billed output tokens reported by Spark.
    completion_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'completionTokens'})
    # TotalTokens is the total token count reported by Spark.
    total_tokens: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalTokens'})


setattr(ChatCompletion, '__module__', 'dex_connectors.iflytek.models')
