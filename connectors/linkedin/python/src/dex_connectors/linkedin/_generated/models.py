# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetAuthenticatedProfileInput contains the provider request fields for get authenticated profile.
@dataclass(frozen=True, slots=True)
class GetAuthenticatedProfileInput:
    pass


setattr(GetAuthenticatedProfileInput, '__module__', 'dex_connectors.linkedin.models')


# AuthenticatedProfile represents the connector's authenticated profile data.
@dataclass(frozen=True, slots=True)
class AuthenticatedProfile:
    # Subject is the message subject.
    subject: str = ''
    # Name is the name for authenticated profile.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # GivenName is the given name for authenticated profile.
    given_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'givenName', 'dex_json_omit_default': True})
    # FamilyName is the family name for authenticated profile.
    family_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'familyName', 'dex_json_omit_default': True})
    # PictureURL is the picture URL for authenticated profile.
    picture_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'pictureUrl', 'dex_json_omit_default': True})
    # Locale is the locale for authenticated profile.
    locale: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # VerifiedEmail is the verified email for authenticated profile.
    verified_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'verifiedEmail'})


setattr(AuthenticatedProfile, '__module__', 'dex_connectors.linkedin.models')
