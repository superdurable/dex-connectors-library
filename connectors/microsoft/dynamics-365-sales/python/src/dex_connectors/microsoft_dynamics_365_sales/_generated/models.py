# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# FindContactInput selects one contact by its primary email address.
@dataclass(frozen=True, slots=True)
class FindContactInput:
    # EmailAddress is the contact's exact primary email address.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress'})


setattr(FindContactInput, '__module__', 'dex_connectors.microsoft_dynamics_365_sales.models')


# Contact is the stable identity and basic display information for one Dataverse contact.
@dataclass(frozen=True, slots=True)
class Contact:
    # ContactID is the Dataverse contact UUID.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId'})
    # FullName is the name shown in Dynamics 365 Sales.
    full_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'fullName'})
    # EmailAddress is the primary email returned by Dataverse.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress'})


setattr(Contact, '__module__', 'dex_connectors.microsoft_dynamics_365_sales.models')
