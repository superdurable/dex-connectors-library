# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetEmailIdentityInput identifies one SES email identity in the configured Region.
@dataclass(frozen=True, slots=True)
class GetEmailIdentityInput:
    email_identity: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailIdentity'})


setattr(GetEmailIdentityInput, '__module__', 'dex_connectors.amazon_ses.models')


# EmailIdentityDetails returns the name and verification status of an SES email identity.
@dataclass(frozen=True, slots=True)
class EmailIdentityDetails:
    name: str = ''
    verification_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'verificationStatus'})


setattr(EmailIdentityDetails, '__module__', 'dex_connectors.amazon_ses.models')
