# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetAccountStatusInput requests the account state associated with the configured key.
@dataclass(frozen=True, slots=True)
class GetAccountStatusInput:
    pass


setattr(GetAccountStatusInput, '__module__', 'dex_connectors.tianxiagongchang.models')


# GetAccountStatusOutput contains only application identity and credit status.
@dataclass(frozen=True, slots=True)
class GetAccountStatusOutput:
    # AppID identifies the application associated with the key.
    app_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'appId'})
    # CreditsBalance is the account-wide available credit balance in provider credits.
    credits_balance: int = _dataclass_field(default=0, metadata={'dex_json_name': 'creditsBalance'})
    # FrozenCredits is the credit amount currently frozen for this application.
    frozen_credits: int = _dataclass_field(default=0, metadata={'dex_json_name': 'frozenCredits'})
    # IsSandbox indicates that the key is a sandbox key.
    is_sandbox: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isSandbox'})


setattr(GetAccountStatusOutput, '__module__', 'dex_connectors.tianxiagongchang.models')
