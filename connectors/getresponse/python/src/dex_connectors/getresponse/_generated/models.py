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


# ListContactsInput selects a one-based page of at most 1000 contacts.
@dataclass(frozen=True, slots=True)
class ListContactsInput:
    # Page is the one-based page number; zero defaults to one.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # PerPage is the maximum count; zero defaults to 20.
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage', 'dex_json_omit_default': True})


setattr(ListContactsInput, '__module__', 'dex_connectors.getresponse.models')


# ContactMetadata contains contact identity and name without embedded fields.
@dataclass(frozen=True, slots=True)
class ContactMetadata:
    # ContactID is the stable GetResponse contact ID.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId'})
    # Email is the contact email address.
    email: str = ''
    # Name is the contact name, when present.
    name: str = ''


setattr(ContactMetadata, '__module__', 'dex_connectors.getresponse.models')


# ContactsPage contains one page of contact metadata.
@dataclass(frozen=True, slots=True)
class ContactsPage:
    # Contacts contains the returned contact records.
    contacts: tuple[ContactMetadata, ...] = ()
    # Page is the requested one-based page.
    page: int = 0
    # PerPage is the requested maximum count.
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'contacts', _replace_nil_slices(self.contacts, 1))


ContactsPage.__annotations__['contacts'] = tuple[ContactMetadata, ...] | None
ContactsPage.__dataclass_fields__['contacts'].type = tuple[ContactMetadata, ...] | None
setattr(ContactsPage, '__module__', 'dex_connectors.getresponse.models')


# GetContactInput selects one GetResponse contact ID.
@dataclass(frozen=True, slots=True)
class GetContactInput:
    # ContactID is the ID from a contact page or API.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId'})


setattr(GetContactInput, '__module__', 'dex_connectors.getresponse.models')
