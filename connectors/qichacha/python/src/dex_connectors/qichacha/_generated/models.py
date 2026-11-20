# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetEnterpriseBasicDetailsInput identifies one mainland enterprise by company name or unified social credit code.
@dataclass(frozen=True, slots=True)
class GetEnterpriseBasicDetailsInput:
    # Keyword is the exact enterprise name or 18-character unified social credit code.
    keyword: str = ''


setattr(GetEnterpriseBasicDetailsInput, '__module__', 'dex_connectors.qichacha.models')


# GetEnterpriseBasicDetailsOutput contains only non-personal enterprise registration fields.
@dataclass(frozen=True, slots=True)
class GetEnterpriseBasicDetailsOutput:
    # Name is the registered enterprise name.
    name: str = ''
    # CreditCode is the mainland unified social credit code, which may be empty when unavailable.
    credit_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'creditCode'})
    # RegistrationStatus is the provider's enterprise registration status text.
    registration_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'registrationStatus'})


setattr(GetEnterpriseBasicDetailsOutput, '__module__', 'dex_connectors.qichacha.models')
