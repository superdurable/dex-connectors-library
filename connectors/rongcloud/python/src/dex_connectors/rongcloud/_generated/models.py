# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# CheckUserOnlineInput identifies one existing RongCloud IM user.
@dataclass(frozen=True, slots=True)
class CheckUserOnlineInput:
    # UserID is the application's opaque user ID, up to 64 ASCII letters or digits.
    user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'userId'})


setattr(CheckUserOnlineInput, '__module__', 'dex_connectors.rongcloud.models')


# CheckUserOnlineOutput contains only the online-state boolean.
@dataclass(frozen=True, slots=True)
class CheckUserOnlineOutput:
    # IsOnline is true when RongCloud reports status "1".
    is_online: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isOnline'})


setattr(CheckUserOnlineOutput, '__module__', 'dex_connectors.rongcloud.models')
