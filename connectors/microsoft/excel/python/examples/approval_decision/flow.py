# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run all four Excel operations in a durable approval decision Flow."""

import dex
from dex_connectors.sdk import MutationResult, QueryResult

from dex_connectors.microsoft_excel import (
    AppendTableRowsInput,
    AppendTableRowsOutput,
    Connection,
    GetTableRowsInput,
    GetTableRowsOutput,
    GetValuesInput,
    GetValuesOutput,
    UpdateValuesInput,
    UpdateValuesOutput,
    new_append_table_rows_step,
    new_get_table_rows_step,
    new_get_values_step,
    new_update_values_step,
)

from .models import CONNECTION_NAME, ApprovalInput, ApprovalOutput

_REQUEST = dex.Attribute("excel-approval-request", ApprovalInput)
_POLICY_ROW_COUNT = dex.Attribute("excel-policy-row-count", int)


# dex:group group-id:excel group-label:"Excel approval"
# dex:explanation text:"Persist workbook locations and start the policy table read."
class PrepareDecision(dex.Step[ApprovalInput]):
    """Store the non-secret request before the first Graph call."""

    async def execute(self, context: dex.AsyncContext, input: ApprovalInput) -> dex.StepDecision:
        """Route one validated application request to the policy read."""
        if not input.request_id or input.amount < 0:
            return dex.force_fail("request ID and nonnegative amount are required")
        _REQUEST.set(context, input)
        return dex.go_to(dex.StepRef("ReadPolicyRows"), input)


# dex:group group-id:excel group-label:"Excel approval"
# dex:explanation text:"Retain the policy row count and read the current summary cells."
class PolicyRowsRead(dex.Step[QueryResult[GetTableRowsOutput]]):
    """Continue after the policy table was read."""

    async def execute(
        self, context: dex.AsyncContext, input: QueryResult[GetTableRowsOutput]
    ) -> dex.StepDecision:
        """Persist the row count for the final decision result."""
        _POLICY_ROW_COUNT.set(context, len(input.value.rows))
        return dex.go_to(dex.StepRef("ReadSummaryValues"), _REQUEST.get(context))


# dex:group group-id:excel group-label:"Excel approval"
# dex:explanation text:"Write the literal decision after the summary range is readable."
class SummaryValuesRead(dex.Step[QueryResult[GetValuesOutput]]):
    """Continue after the summary range was read."""

    async def execute(
        self, context: dex.AsyncContext, input: QueryResult[GetValuesOutput]
    ) -> dex.StepDecision:
        """Route the stored application request to the absolute range write."""
        return dex.go_to(dex.StepRef("WriteSummaryValues"), _REQUEST.get(context))


# dex:group group-id:excel group-label:"Excel approval"
# dex:explanation text:"Append the decision row after the absolute summary write succeeds."
class SummaryValuesUpdated(dex.Step[MutationResult[UpdateValuesOutput]]):
    """Continue after Excel confirms the summary write."""

    async def execute(
        self, context: dex.AsyncContext, input: MutationResult[UpdateValuesOutput]
    ) -> dex.StepDecision:
        """Route the stored request to the duplicate-safe append."""
        return dex.go_to(dex.StepRef("AppendDecisionRow"), _REQUEST.get(context))


# dex:group group-id:excel group-label:"Excel approval"
# dex:explanation text:"Complete after the table contains the requested decision key."
class DecisionRowAppended(dex.Step[MutationResult[AppendTableRowsOutput]]):
    """Complete once Excel holds the decision row."""

    async def execute(
        self, context: dex.AsyncContext, input: MutationResult[AppendTableRowsOutput]
    ) -> dex.StepDecision:
        """Return only request identity and confirmed business effects."""
        request = _REQUEST.get(context)
        return dex.graceful_complete(
            ApprovalOutput(
                request.request_id, bool(input.value.appended_keys), _POLICY_ROW_COUNT.get(context)
            )
        )


class ApprovalDecisionFlow(dex.Flow[ApprovalInput]):
    """Read policy and summary, then write and append with one named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("approval Flow requires the static approvals connection")
        self.prepare = PrepareDecision()
        self.policy_rows_read = PolicyRowsRead()
        self.summary_values_read = SummaryValuesRead()
        self.summary_values_updated = SummaryValuesUpdated()
        self.decision_row_appended = DecisionRowAppended()
        annotations = {
            "groupId": "excel",
            "groupLabel": "Excel approval",
            "explanation": "Read or write the selected Microsoft Excel workbook.",
        }
        self.policy = new_get_table_rows_step(
            step_type="ReadPolicyRows",
            step_input_type=ApprovalInput,
            annotations=annotations,
            connection=connection,
            connection_name="approvals",
            read=self.policy_rows_read,
            map_to_operation_input=lambda value: GetTableRowsInput(
                value.drive_id, value.workbook_id, value.policy_table
            ),
        )
        self.summary = new_get_values_step(
            step_type="ReadSummaryValues",
            step_input_type=ApprovalInput,
            annotations=annotations,
            connection=connection,
            connection_name="approvals",
            read=self.summary_values_read,
            map_to_operation_input=lambda value: GetValuesInput(
                value.drive_id, value.workbook_id, value.summary_worksheet, value.summary_address
            ),
        )
        self.write = new_update_values_step(
            step_type="WriteSummaryValues",
            step_input_type=ApprovalInput,
            annotations=annotations,
            connection=connection,
            connection_name="approvals",
            updated=self.summary_values_updated,
            map_to_operation_input=lambda value: UpdateValuesInput(
                value.drive_id,
                value.workbook_id,
                value.summary_worksheet,
                value.summary_address,
                ((value.request_id, value.amount),),
            ),
        )
        self.append = new_append_table_rows_step(
            step_type="AppendDecisionRow",
            step_input_type=ApprovalInput,
            annotations=annotations,
            connection=connection,
            connection_name="approvals",
            appended=self.decision_row_appended,
            map_to_operation_input=lambda value: AppendTableRowsInput(
                value.drive_id,
                value.workbook_id,
                value.decision_table,
                "RequestId",
                ({"RequestId": value.request_id, "Amount": value.amount},),
            ),
        )

    def get_steps(self) -> dex.StepList[ApprovalInput]:
        """Register each Step once under a stable identity."""
        return dex.StepList.start_step(self.prepare).other_steps(
            self.policy,
            self.policy_rows_read,
            self.summary,
            self.summary_values_read,
            self.write,
            self.summary_values_updated,
            self.append,
            self.decision_row_appended,
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the non-secret request and policy row count across Steps."""
        return dex.PersistenceSchema.of(_REQUEST, _POLICY_ROW_COUNT)
