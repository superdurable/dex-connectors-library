# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetUserProfileInput selects a Braze user by the application's external ID.
@dataclass(frozen=True, slots=True)
class GetUserProfileInput:
    # ExternalID is a non-secret stable application user ID.
    external_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'externalId'})


setattr(GetUserProfileInput, '__module__', 'dex_connectors.braze.models')


# UserProfile contains only the selected profile fields, excluding custom attributes.
@dataclass(frozen=True, slots=True)
class UserProfile:
    # ExternalID is the application-supplied user ID.
    external_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'externalId'})
    # BrazeID is Braze's internal user ID.
    braze_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'brazeId', 'dex_json_omit_default': True})
    # FirstName is the optional given name.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName', 'dex_json_omit_default': True})
    # LastName is the optional family name.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName', 'dex_json_omit_default': True})
    # Email is the optional email address.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(UserProfile, '__module__', 'dex_connectors.braze.models')
