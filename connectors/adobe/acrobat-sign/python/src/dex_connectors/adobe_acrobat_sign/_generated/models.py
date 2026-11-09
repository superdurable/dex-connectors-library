# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetAgreementInput selects an agreement by its non-secret ID.
@dataclass(frozen=True, slots=True)
class GetAgreementInput:
    # AgreementID is the ID returned when Acrobat Sign creates the agreement.
    agreement_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'agreementId'})


setattr(GetAgreementInput, '__module__', 'dex_connectors.adobe_acrobat_sign.models')


# AgreementStatus excludes participants and document contents.
@dataclass(frozen=True, slots=True)
class AgreementStatus:
    # ID is the Acrobat Sign agreement ID.
    id: str = ''
    # Name is the agreement title.
    name: str = ''
    # Status is an Acrobat Sign agreement state such as SIGNED.
    status: str = ''


setattr(AgreementStatus, '__module__', 'dex_connectors.adobe_acrobat_sign.models')
