// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sageintacct;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sageintacct.InvoiceDirectory.*;
import io.superdurable.dexconnectors.sdk.*;

/** Read the selected invoice. */
public final class GetInvoiceFlow implements Flow<GetInvoiceInput> {
    private final QueryStep<GetInvoiceInput, GetInvoiceInput, InvoiceSummary, GetInvoiceResult>
            query;
    private final ReturnInvoiceSummary complete = new ReturnInvoiceSummary();

    /** Bind the Sage Intacct query when the Worker registers this Flow. */
    public GetInvoiceFlow(Query<GetInvoiceInput, InvoiceSummary> operation) {
        query =
                new QueryStep<GetInvoiceInput, GetInvoiceInput, InvoiceSummary, GetInvoiceResult>(
                        InvoiceDirectory.newGetInvoiceStep(
                                        "GetInvoice",
                                        GetInvoiceInput.class,
                                        new StepAnnotations(
                                                "invoices",
                                                "Invoices",
                                                "Read the selected invoice."),
                                        operation,
                                        new ConnectionRef("sage-intacct", "sage-intacct"),
                                        input -> input)
                                .branch("found", ReturnInvoiceSummary.class)
                                .build()) {};
    }

    @Override
    public String getFlowType() {
        return "JavaSageIntacctGetInvoice";
    }

    @Override
    public StepList<GetInvoiceInput> getSteps() {
        return StepList.startStep(query).otherSteps(complete);
    }

    @Override
    public PersistenceSchema getPersistenceSchema() {
        return PersistenceSchema.of();
    }

    // dex:group group-id:invoices group-label:"Invoices"
    // dex:explanation text:"Return the invoice keys and IDs."
    public static final class ReturnInvoiceSummary implements Step<GetInvoiceResult> {
        @Override
        public Class<GetInvoiceResult> getInputType() {
            return GetInvoiceResult.class;
        }

        @Override
        public StepDecision execute(Context context, GetInvoiceResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
