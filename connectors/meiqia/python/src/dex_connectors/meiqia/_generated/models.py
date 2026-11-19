# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetConversationInput identifies one Meiqia conversation.
@dataclass(frozen=True, slots=True)
class GetConversationInput:
    # ConversationID is a positive provider conversation identifier.
    conversation_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'conversationID'})


setattr(GetConversationInput, '__module__', 'dex_connectors.meiqia.models')


# GetConversationOutput contains only non-content conversation metadata.
@dataclass(frozen=True, slots=True)
class GetConversationOutput:
    # ConversationID is the provider conversation identifier.
    conversation_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'conversationID'})
    # StartedAt is the provider's conversation start time.
    started_at: str = _dataclass_field(default='', metadata={'dex_json_name': 'startedAt'})
    # EndedAt is the provider's conversation end time when available.
    ended_at: str = _dataclass_field(default='', metadata={'dex_json_name': 'endedAt'})
    # MainChannel is the provider's channel name when available.
    main_channel: str = _dataclass_field(default='', metadata={'dex_json_name': 'mainChannel'})


setattr(GetConversationOutput, '__module__', 'dex_connectors.meiqia.models')
