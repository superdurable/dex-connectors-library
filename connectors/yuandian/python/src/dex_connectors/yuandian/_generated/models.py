# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetCompanyProfileInput identifies one company by its unified social credit code.
@dataclass(frozen=True, slots=True)
class GetCompanyProfileInput:
    # CreditCode is the 18-character uppercase unified social credit code.
    credit_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'creditCode'})


setattr(GetCompanyProfileInput, '__module__', 'dex_connectors.yuandian.models')


# GetCompanyProfileOutput retains selected company registration facts only.
@dataclass(frozen=True, slots=True)
class GetCompanyProfileOutput:
    # CompanyID is the provider's company identifier.
    company_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'companyID'})
    # Name is the registered company name.
    name: str = ''
    # CreditCode is the unified social credit code.
    credit_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'creditCode'})
    # RegistrationStatus is the registered operating status.
    registration_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'registrationStatus'})
    # Industry is the provider's industry classification.
    industry: str = ''
    # CompanyType is the registered organization type.
    company_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'companyType'})


setattr(GetCompanyProfileOutput, '__module__', 'dex_connectors.yuandian.models')
