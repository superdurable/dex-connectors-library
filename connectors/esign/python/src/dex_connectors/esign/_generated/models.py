# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetSignFlowStatusInput identifies an existing signing flow.
@dataclass(frozen=True, slots=True)
class GetSignFlowStatusInput:
    # SignFlowID is the opaque flow ID returned by an existing eSign signing workflow.
    sign_flow_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'signFlowId'})


setattr(GetSignFlowStatusInput, '__module__', 'dex_connectors.esign.models')


# GetSignFlowStatusOutput contains only the signing flow ID and numeric status.
@dataclass(frozen=True, slots=True)
class GetSignFlowStatusOutput:
    # SignFlowID is the requested signing flow identifier.
    sign_flow_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'signFlowId'})
    # SignFlowStatus is eSign's numeric signing-flow state.
    sign_flow_status: int = _dataclass_field(default=0, metadata={'dex_json_name': 'signFlowStatus'})


setattr(GetSignFlowStatusOutput, '__module__', 'dex_connectors.esign.models')
