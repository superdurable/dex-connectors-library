# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Flow that finds the one refund policy row whose Policy Key equals a requested key.

It is the policy lookup Step of the Go refund-decision example, with the base and table passed
as start input instead of Studio pickers.
"""

from dataclasses import dataclass, field, replace

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.airtable import (
    Connection,
    FieldEqualityFilter,
    ListRecordsInput,
    Record,
    RecordPage,
    new_list_records_step,
)

FLOW_TYPE = "AirtablePolicyLookup"
CONNECTION_NAME = "airtable-refund-policies"
_FIND_POLICY_STEP_TYPE = "FindAirtableRefundPolicy"
_POLICY_KEY_FIELD = "Policy Key"


@dataclass(frozen=True, slots=True)
class PolicyLookupRequest:
    """Base ID such as appXXXXXXXXXXXXXX, table ID or name, and the Policy Key text to match."""

    base_id: str = field(default="", metadata={"dex_json_name": "baseId"})
    table_id_or_name: str = field(default="", metadata={"dex_json_name": "tableIdOrName"})
    policy_key: str = field(default="", metadata={"dex_json_name": "policyKey"})


@dataclass(frozen=True, slots=True)
class PolicyLookupOutcome:
    """The single matching policy record, or isAmbiguous when zero or several rows matched."""

    policy: Record | None = field(default=None, metadata={"dex_json_omit_default": True})
    is_ambiguous: bool = field(default=False, metadata={"dex_json_name": "isAmbiguous"})


_REQUEST = dex.Attribute("airtable-policy-lookup-request", PolicyLookupRequest)
_OUTCOME = dex.Attribute("airtable-policy-lookup-outcome", PolicyLookupOutcome)


def map_to_list_records_input(request: PolicyLookupRequest) -> ListRecordsInput:
    """List at most two rows whose Policy Key equals the key, so a duplicate key is detected."""
    return ListRecordsInput(
        base_id=request.base_id,
        table_id_or_name=request.table_id_or_name,
        filters=(FieldEqualityFilter(field=_POLICY_KEY_FIELD, text=request.policy_key),),
        page_size=2,
    )


# dex:group group-id:airtable group-label:"Airtable"
# dex:explanation text:"Validate and record the policy lookup before calling Airtable."
class _RecordPolicyLookupRequest(dex.Step[PolicyLookupRequest]):
    def get_step_type(self) -> str:
        return "RecordAirtablePolicyLookupRequest"

    def execute(self, context: dex.Context, input: PolicyLookupRequest) -> dex.StepDecision:
        request = replace(input, policy_key=input.policy_key.strip())
        if not request.base_id or not request.table_id_or_name or not request.policy_key:
            return dex.force_fail("baseId, tableIdOrName, and policyKey are required")
        _REQUEST.set(context, request)
        return dex.go_to(dex.StepRef("FindAirtableRefundPolicy"), request)


# dex:group group-id:airtable group-label:"Airtable"
# dex:explanation text:"Complete with the one matching policy, or mark the lookup ambiguous."
class _CompletePolicyLookup(dex.Step[QueryResult[RecordPage]]):
    def get_step_type(self) -> str:
        return "CompleteAirtablePolicyLookup"

    def execute(self, context: dex.Context, input: QueryResult[RecordPage]) -> dex.StepDecision:
        records = input.value.records
        outcome = (
            PolicyLookupOutcome(records[0]) if len(records) == 1 else PolicyLookupOutcome(is_ambiguous=True)
        )
        _OUTCOME.set(context, outcome)
        return dex.graceful_complete(outcome)


class PolicyLookupFlow(dex.Flow[PolicyLookupRequest]):
    """Find one refund policy row through the static airtable-refund-policies connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("policy lookup requires the static airtable-refund-policies connection")
        self._record_request = _RecordPolicyLookupRequest()
        self._complete = _CompletePolicyLookup()
        self._find = new_list_records_step(
            step_type=_FIND_POLICY_STEP_TYPE,
            step_input_type=PolicyLookupRequest,
            annotations={
                "groupId": "airtable",
                "groupLabel": "Airtable",
                "explanation": "Find the refund policy whose Policy Key equals the requested key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_list_records_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "AirtablePolicyLookup"

    def get_steps(self) -> dex.StepList[PolicyLookupRequest]:
        """Register the request, policy lookup, and completion Steps."""
        return dex.StepList.start_step(self._record_request).other_steps(self._find, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the request and outcome."""
        return dex.PersistenceSchema.of(_REQUEST, _OUTCOME)
