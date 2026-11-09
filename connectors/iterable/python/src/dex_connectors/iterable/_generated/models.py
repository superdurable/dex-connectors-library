# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetUserInput selects a user by a non-secret email address.
@dataclass(frozen=True, slots=True)
class GetUserInput:
    # Email is the user's email in an Iterable email-based or hybrid project.
    email: str = ''


setattr(GetUserInput, '__module__', 'dex_connectors.iterable.models')


# UserIdentity excludes arbitrary profile data fields.
@dataclass(frozen=True, slots=True)
class UserIdentity:
    # Email is the selected user's email address.
    email: str = ''
    # UserID is Iterable's optional stable user identifier.
    user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'userId'})


setattr(UserIdentity, '__module__', 'dex_connectors.iterable.models')
