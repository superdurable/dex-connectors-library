# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetContactInput selects a contact by the UUID in its V3 API resource.
@dataclass(frozen=True, slots=True)
class GetContactInput:
    # ContactID is a non-secret V3 contact UUID.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId'})


setattr(GetContactInput, '__module__', 'dex_connectors.constantcontact.models')


# ContactMetadata contains one contact's identity and optional email permission state.
@dataclass(frozen=True, slots=True)
class ContactMetadata:
    # ContactID is the V3 contact UUID.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId'})
    # EmailAddress is absent for a contact with no email channel.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress', 'dex_json_omit_default': True})
    # FirstName is the contact's optional given name.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName', 'dex_json_omit_default': True})
    # LastName is the contact's optional family name.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName', 'dex_json_omit_default': True})
    # EmailPermission is Constant Contact's permission_to_send state when an email channel exists.
    email_permission: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailPermission', 'dex_json_omit_default': True})


setattr(ContactMetadata, '__module__', 'dex_connectors.constantcontact.models')
