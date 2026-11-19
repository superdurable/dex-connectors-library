# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetAuthorizedUserInput has no fields because the access token identifies the user.
@dataclass(frozen=True, slots=True)
class GetAuthorizedUserInput:
    pass


setattr(GetAuthorizedUserInput, '__module__', 'dex_connectors.bilibili.models')


# AuthorizedUser is the user's public identity scoped to the application.
@dataclass(frozen=True, slots=True)
class AuthorizedUser:
    # OpenID is the stable application-scoped user identifier.
    open_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'openID'})
    # Name is the user's public nickname.
    name: str = ''


setattr(AuthorizedUser, '__module__', 'dex_connectors.bilibili.models')
