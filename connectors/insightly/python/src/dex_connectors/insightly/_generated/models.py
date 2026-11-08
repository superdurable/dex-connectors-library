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


# ListContactsInput selects a bounded Insightly contact page.
@dataclass(frozen=True, slots=True)
class ListContactsInput:
    # Top is the maximum page size, from 1 to 500 records.
    top: int = 0
    # Skip is the nonnegative number of contacts to omit before this page.
    skip: int = 0


setattr(ListContactsInput, '__module__', 'dex_connectors.insightly.models')


# ContactMetadata contains public contact identity without custom fields or links.
@dataclass(frozen=True, slots=True)
class ContactMetadata:
    # ID is the stable Insightly contact identifier.
    id: int = 0
    # FirstName is the contact's given name when available.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName'})
    # LastName is the contact's family name when available.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName'})
    # EmailAddress is the contact's primary email address when available.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress'})


setattr(ContactMetadata, '__module__', 'dex_connectors.insightly.models')


# ContactsPage contains the requested contact page.
@dataclass(frozen=True, slots=True)
class ContactsPage:
    # Contacts contains only contact identity fields.
    contacts: tuple[ContactMetadata, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'contacts', _replace_nil_slices(self.contacts, 1))


ContactsPage.__annotations__['contacts'] = tuple[ContactMetadata, ...] | None
ContactsPage.__dataclass_fields__['contacts'].type = tuple[ContactMetadata, ...] | None
setattr(ContactsPage, '__module__', 'dex_connectors.insightly.models')


# GetContactInput selects one Insightly contact by its numeric ID.
@dataclass(frozen=True, slots=True)
class GetContactInput:
    # ContactID is the decimal ID shown in Insightly and returned by its API.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId'})


setattr(GetContactInput, '__module__', 'dex_connectors.insightly.models')
