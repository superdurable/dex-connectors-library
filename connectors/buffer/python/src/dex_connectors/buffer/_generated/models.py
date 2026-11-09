# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetChannelInput selects a connected Buffer channel by opaque ID.
@dataclass(frozen=True, slots=True)
class GetChannelInput:
    # ChannelID is the ID returned by Buffer channel queries.
    channel_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelId'})


setattr(GetChannelInput, '__module__', 'dex_connectors.buffer.models')


# ChannelProfile contains public metadata for one connected channel.
@dataclass(frozen=True, slots=True)
class ChannelProfile:
    # ID is the Buffer channel ID.
    id: str = ''
    # Name is the connected channel name.
    name: str = ''
    # Service is the social platform identifier.
    service: str = ''


setattr(ChannelProfile, '__module__', 'dex_connectors.buffer.models')
