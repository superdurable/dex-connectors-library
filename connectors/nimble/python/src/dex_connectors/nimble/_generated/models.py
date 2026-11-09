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


# ListContactIDsInput selects one Nimble contact ID page.
@dataclass(frozen=True, slots=True)
class ListContactIDsInput:
    # Page is the one-based page number.
    page: int = 0
    # PerPage is the bounded page size, from 1 through 100.
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage'})


setattr(ListContactIDsInput, '__module__', 'dex_connectors.nimble.models')


# ContactIDsPage contains one page of stable contact IDs and pagination metadata.
@dataclass(frozen=True, slots=True)
class ContactIDsPage:
    # IDs are the contact identifiers returned by Nimble.
    ids: tuple[str, ...] = ()
    # Page is the returned one-based page number.
    page: int = 0
    # Pages is the total number of available pages.
    pages: int = 0
    # PerPage is the provider's page size.
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage'})
    # Total is the total number of matching contacts.
    total: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, 'ids', _replace_nil_slices(self.ids, 1))


ContactIDsPage.__annotations__['ids'] = tuple[str, ...] | None
ContactIDsPage.__dataclass_fields__['ids'].type = tuple[str, ...] | None
setattr(ContactIDsPage, '__module__', 'dex_connectors.nimble.models')


# GetContactInput selects one Nimble contact by its BSON identifier.
@dataclass(frozen=True, slots=True)
class GetContactInput:
    # ContactID is the 24-character hexadecimal ID returned by Nimble.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId'})


setattr(GetContactInput, '__module__', 'dex_connectors.nimble.models')


# ContactMetadata contains selected contact identity without custom fields or links.
@dataclass(frozen=True, slots=True)
class ContactMetadata:
    # ID is the stable Nimble contact identifier.
    id: str = ''
    # FirstName is the first name when present.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName'})
    # LastName is the last name when present.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName'})
    # EmailAddress is the first email value when present.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress'})


setattr(ContactMetadata, '__module__', 'dex_connectors.nimble.models')
