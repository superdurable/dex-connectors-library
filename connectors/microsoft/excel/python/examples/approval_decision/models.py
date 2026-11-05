# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Flow input and output for one approval decision."""

from dataclasses import dataclass

CONNECTION_NAME = "approvals"


@dataclass(frozen=True, slots=True)
class ApprovalInput:
    """Workbook locations and literal decision values chosen by the application."""

    drive_id: str
    workbook_id: str
    policy_table: str
    summary_worksheet: str
    summary_address: str
    decision_table: str
    request_id: str
    amount: int


@dataclass(frozen=True, slots=True)
class ApprovalOutput:
    """Confirmed table and summary effects without connection credentials."""

    request_id: str
    appended: bool
    existing_policy_rows: int
