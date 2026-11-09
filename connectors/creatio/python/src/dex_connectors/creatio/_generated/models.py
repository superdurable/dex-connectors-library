# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetContactInput identifies one Creatio contact by UUID.
@dataclass(frozen=True, slots=True)
class GetContactInput:
    # ContactID is the Contact.Id value from the Creatio Contacts page or OData.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId'})


setattr(GetContactInput, '__module__', 'dex_connectors.creatio.models')


# ContactSummary contains selected Creatio contact fields.
@dataclass(frozen=True, slots=True)
class ContactSummary:
    # ID is the stable Contact.Id UUID.
    id: str = ''
    # Name is the contact's displayed full name.
    name: str = ''
    # Email is the contact email, or blank when absent.
    email: str = ''


setattr(ContactSummary, '__module__', 'dex_connectors.creatio.models')
