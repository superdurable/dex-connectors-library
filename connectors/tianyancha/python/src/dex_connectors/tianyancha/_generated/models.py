# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetCompanyProfileInput identifies one company by name, ID, registration number, or credit code.
@dataclass(frozen=True, slots=True)
class GetCompanyProfileInput:
    # Keyword is one company name, ID, registration number, or unified social credit code.
    keyword: str = ''


setattr(GetCompanyProfileInput, '__module__', 'dex_connectors.tianyancha.models')


# GetCompanyProfileOutput includes selected public business registration facts only.
@dataclass(frozen=True, slots=True)
class GetCompanyProfileOutput:
    # CompanyID is Tianyancha's company identifier.
    company_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'companyID'})
    # Name is the registered company name.
    name: str = ''
    # RegistrationStatus is the provider's company status.
    registration_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'registrationStatus'})
    # CreditCode is the unified social credit code.
    credit_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'creditCode'})
    # Industry is the provider's industry classification.
    industry: str = ''
    # RegisteredCapital is the textual capital amount and currency.
    registered_capital: str = _dataclass_field(default='', metadata={'dex_json_name': 'registeredCapital'})
    # CompanyType is the registered organization type.
    company_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'companyType'})


setattr(GetCompanyProfileOutput, '__module__', 'dex_connectors.tianyancha.models')
