# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetUserDetailInput selects one IM user by ID.
@dataclass(frozen=True, slots=True)
class GetUserDetailInput:
    # UserID is the registered IM user ID, with lowercase letters, digits, underscore, hyphen, or period.
    user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'userId'})


setattr(GetUserDetailInput, '__module__', 'dex_connectors.easemob.models')


# UserDetail contains selected nonsecret Easemob account fields.
@dataclass(frozen=True, slots=True)
class UserDetail:
    # UserID is the registered IM user ID.
    user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'userId'})
    # UserUUID is Easemob's internal identifier for this user.
    user_uuid: str = _dataclass_field(default='', metadata={'dex_json_name': 'userUuid'})
    # CreatedAtMilliseconds is account creation time in Unix milliseconds.
    created_at_milliseconds: int = _dataclass_field(default=0, metadata={'dex_json_name': 'createdAtMilliseconds'})
    # ModifiedAtMilliseconds is the last account modification time in Unix milliseconds.
    modified_at_milliseconds: int = _dataclass_field(default=0, metadata={'dex_json_name': 'modifiedAtMilliseconds'})
    # Activated reports whether the account can be used.
    activated: bool = False
    # Nickname is the optional push notification nickname.
    nickname: str = ''


setattr(UserDetail, '__module__', 'dex_connectors.easemob.models')
