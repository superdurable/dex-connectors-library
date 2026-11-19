# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetUserProfileInput selects one IM account for a read-only profile lookup.
@dataclass(frozen=True, slots=True)
class GetUserProfileInput:
    # AccountID is the account ID assigned when the IM user was registered.
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountId'})


setattr(GetUserProfileInput, '__module__', 'dex_connectors.netease.models')


# GetUserProfileOutput excludes private contact and demographic profile fields.
@dataclass(frozen=True, slots=True)
class GetUserProfileOutput:
    # AccountID is the IM account ID.
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountId'})
    # Name is the optional nickname, or empty when the user has none.
    name: str = ''


setattr(GetUserProfileOutput, '__module__', 'dex_connectors.netease.models')
