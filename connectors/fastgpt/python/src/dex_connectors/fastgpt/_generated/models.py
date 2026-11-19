# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

# GetDatasetDetailsInput identifies one knowledge base visible to the personal API Key.
@dataclass(frozen=True, slots=True)
class GetDatasetDetailsInput:
    # ID is the knowledge base identifier copied from its FastGPT URL.
    id: str = ''


setattr(GetDatasetDetailsInput, '__module__', 'dex_connectors.fastgpt.models')


# GetDatasetDetailsOutput contains only knowledge base metadata, excluding models and content.
@dataclass(frozen=True, slots=True)
class GetDatasetDetailsOutput:
    # ID is the provider's knowledge base identifier.
    id: str = ''
    # Name is the knowledge base title.
    name: str = ''
    # Type is the provider's knowledge base type.
    type: str = ''
    # Status is the provider's knowledge base status.
    status: str = ''


setattr(GetDatasetDetailsOutput, '__module__', 'dex_connectors.fastgpt.models')
