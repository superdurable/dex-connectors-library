# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public aliases of the provider-neutral SDK text generation contract."""

from dataclasses import dataclass

from dex_connectors.sdk.textgen import (
    Message as textgenMessage,
)
from dex_connectors.sdk.textgen import (
    StructuredOutput as textgenStructuredOutput,
)
from dex_connectors.sdk.textgen import (
    TextGenerationRequest as GenerateTextRequest,
)
from dex_connectors.sdk.textgen import (
    TextGenerationResponse as GenerateTextResponse,
)
from dex_connectors.sdk.textgen import (
    Usage as textgenUsage,
)

Message = textgenMessage
StructuredOutput = textgenStructuredOutput
Usage = textgenUsage


@dataclass(frozen=True, slots=True)
class GenerateTextInput(GenerateTextRequest):
    """Convert JSON scenario children into SDK request values."""

    def __post_init__(self) -> None:
        object.__setattr__(
            self,
            "messages",
            tuple(Message(**message) if isinstance(message, dict) else message for message in self.messages),
        )
        if isinstance(self.structured_output, dict):
            output = self.structured_output
            object.__setattr__(
                self,
                "structured_output",
                StructuredOutput(output.get("name", ""), output.get("schema"), output.get("description", "")),
            )


__all__ = [
    "GenerateTextInput",
    "GenerateTextRequest",
    "GenerateTextResponse",
    "Message",
    "StructuredOutput",
    "Usage",
    "textgenMessage",
    "textgenStructuredOutput",
    "textgenUsage",
]
