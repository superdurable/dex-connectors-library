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


# ProfileClaims is the connected member's bounded LinkedIn OpenID Connect UserInfo profile. Unlike
# AuthenticatedProfile it keeps an unverified or absent email, since a change of verification is itself
# a profile change.
@dataclass(frozen=True, slots=True)
class ProfileClaims:
    # Subject is LinkedIn's sub claim: the member's identifier, stable for this LinkedIn app.
    subject: str = ''
    # Name is the full name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # GivenName is the first name.
    given_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'givenName', 'dex_json_omit_default': True})
    # FamilyName is the last name.
    family_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'familyName', 'dex_json_omit_default': True})
    # Email is the lower-cased primary email address; empty when LinkedIn returns none or an invalid one.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # EmailVerified reports LinkedIn's email_verified claim; false when LinkedIn omits it.
    email_verified: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'emailVerified'})
    # PictureURL is the HTTPS profile picture URL as LinkedIn returned it, including any signed query.
    picture_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'pictureUrl', 'dex_json_omit_default': True})
    # Locale is the member's locale, such as en-US.
    locale: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ProfileClaims, '__module__', 'dex_connectors.linkedin.models')


# ProfileChangedEvent is one change of the connected member's own profile claims between two polls.
@dataclass(frozen=True, slots=True)
class ProfileChangedEvent:
    # Subject is the member whose profile changed; the connection's own member, never another one.
    subject: str = ''
    # Previous is the profile the previous poll returned.
    previous: ProfileClaims = _dataclass_field(default_factory=ProfileClaims)
    # Current is the profile this poll returned.
    current: ProfileClaims = _dataclass_field(default_factory=ProfileClaims)
    # ChangedFields lists the claims that differ, in the order of the ProfileField constants.
    changed_fields: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'changedFields'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'changed_fields', _replace_nil_slices(self.changed_fields, 1))


ProfileChangedEvent.__annotations__['changed_fields'] = tuple[str, ...] | None
ProfileChangedEvent.__dataclass_fields__['changed_fields'].type = tuple[str, ...] | None
setattr(ProfileChangedEvent, '__module__', 'dex_connectors.linkedin.models')


# ProfileChangedTriggerConfiguration is the profileChanged binding configuration. The binding always
# watches the connected member's own profile, so it has no field and Dex Web shows none.
@dataclass(frozen=True, slots=True)
class ProfileChangedTriggerConfiguration:
    pass


setattr(ProfileChangedTriggerConfiguration, '__module__', 'dex_connectors.linkedin.models')
