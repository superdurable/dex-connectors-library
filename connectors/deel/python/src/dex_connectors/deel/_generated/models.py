# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetContractInput selects a contract by its non-secret Deel ID.
@dataclass(frozen=True, slots=True)
class GetContractInput:
    # ContractID is the ID shown in Deel Contracts or returned by the API.
    contract_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contractId'})


setattr(GetContractInput, '__module__', 'dex_connectors.deel.models')


# ContractStatus excludes worker, client, and compensation details.
@dataclass(frozen=True, slots=True)
class ContractStatus:
    # ID is the Deel contract ID.
    id: str = ''
    # Type is Deel's contract type such as ongoing_time_based.
    type: str = ''
    # Status is the contract workflow state such as in_progress.
    status: str = ''


setattr(ContractStatus, '__module__', 'dex_connectors.deel.models')
