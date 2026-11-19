# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetSMSBalanceInput has no parameters; the connection identifies the SMS app.
@dataclass(frozen=True, slots=True)
class GetSMSBalanceInput:
    pass


setattr(GetSMSBalanceInput, '__module__', 'dex_connectors.submail.models')


# SMSBalance contains general and transactional SMS credit amounts.
@dataclass(frozen=True, slots=True)
class SMSBalance:
    # GeneralCredits is the documented decimal count of general SMS credits.
    general_credits: str = _dataclass_field(default='', metadata={'dex_json_name': 'generalCredits'})
    # TransactionalCredits is the documented decimal count of transactional SMS credits.
    transactional_credits: str = _dataclass_field(default='', metadata={'dex_json_name': 'transactionalCredits'})


setattr(SMSBalance, '__module__', 'dex_connectors.submail.models')
