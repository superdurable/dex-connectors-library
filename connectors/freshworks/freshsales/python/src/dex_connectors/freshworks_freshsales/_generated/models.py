# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListContactViewsInput selects the visible Freshsales contact views.
@dataclass(frozen=True, slots=True)
class ListContactViewsInput:
    pass


setattr(ListContactViewsInput, '__module__', 'dex_connectors.freshworks_freshsales.models')


# ContactViewMetadata identifies one Freshsales contact view.
@dataclass(frozen=True, slots=True)
class ContactViewMetadata:
    # ID is the stable view identifier used by the contacts view API.
    id: int = 0
    # Name is the label shown in Freshsales.
    name: str = ''


setattr(ContactViewMetadata, '__module__', 'dex_connectors.freshworks_freshsales.models')


# ContactViews contains the contact views visible to the API user.
@dataclass(frozen=True, slots=True)
class ContactViews:
    # Views contains IDs and names without filter internals.
    views: tuple[ContactViewMetadata, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'views', _replace_nil_slices(self.views, 1))


ContactViews.__annotations__['views'] = tuple[ContactViewMetadata, ...] | None
ContactViews.__dataclass_fields__['views'].type = tuple[ContactViewMetadata, ...] | None
setattr(ContactViews, '__module__', 'dex_connectors.freshworks_freshsales.models')


# GetContactInput selects one Freshsales contact by numeric ID.
@dataclass(frozen=True, slots=True)
class GetContactInput:
    # ContactID is the decimal ID shown by Freshsales and its API.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId'})


setattr(GetContactInput, '__module__', 'dex_connectors.freshworks_freshsales.models')


# ContactMetadata contains contact identity without custom fields or links.
@dataclass(frozen=True, slots=True)
class ContactMetadata:
    # ID is the stable Freshsales contact identifier.
    id: int = 0
    # FirstName is the contact's given name when available.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName'})
    # LastName is the contact's family name when available.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName'})
    # EmailAddress is the contact's email address when available.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress'})


setattr(ContactMetadata, '__module__', 'dex_connectors.freshworks_freshsales.models')
