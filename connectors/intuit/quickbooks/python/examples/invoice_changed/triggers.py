# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Route the invoiceChanged binding to the invoice change Flow through a durable inbox."""

import dex
from dex_connectors.sdk import TriggerLogger, new_dex_flow_trigger_target
from dex_connectors.sdk.pollingtrigger import PollRunner
from dex_connectors.sdk.projectconfig import LoadedProject

from dex_connectors.intuit_quickbooks import (
    Connection,
    ProjectInvoiceChangedTriggerRoute,
    ProjectInvoiceTriggerRunnerConfig,
    new_project_invoice_trigger_runner,
)

from .flow import (
    INVOICE_CHANGED_TRIGGER_BINDING,
    InvoiceChangedFlow,
    accepts_every_invoice_change,
    map_to_invoice_change,
    resolve_invoice_change_flow_id,
)


def new_invoice_change_trigger_runner(
    project: LoadedProject,
    connection: Connection,
    dex_client: dex.AsyncClient,
    flow: InvoiceChangedFlow,
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Poll the company's invoices and start one Flow per invoice version.

    Each change is recorded in the binding's project Trigger inbox before the watermark moves past it.
    """
    target = new_dex_flow_trigger_target(
        dex_client,
        flow,
        accepts_event=accepts_every_invoice_change,
        resolve_flow_id=resolve_invoice_change_flow_id,
        map_to_flow_input=map_to_invoice_change,
    )
    route = ProjectInvoiceChangedTriggerRoute(INVOICE_CHANGED_TRIGGER_BINDING, target)
    return new_project_invoice_trigger_runner(
        project, connection, ProjectInvoiceTriggerRunnerConfig([route], logger=logger)
    )
