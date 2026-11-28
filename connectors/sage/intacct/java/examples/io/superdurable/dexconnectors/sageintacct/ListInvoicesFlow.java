// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sageintacct;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sageintacct.InvoiceDirectory.*;
import io.superdurable.dexconnectors.sdk.*;

/** Read the first invoice page. */
public final class ListInvoicesFlow implements Flow<ListInvoicesInput> {
    private final QueryStep<ListInvoicesInput, ListInvoicesInput, InvoiceList, ListInvoicesResult>
            query;
    private final ReturnInvoiceList complete = new ReturnInvoiceList();

    /** Bind the Sage Intacct query when the Worker registers this Flow. */
    public ListInvoicesFlow(Query<ListInvoicesInput, InvoiceList> operation) {
        query =
                new QueryStep<
                        ListInvoicesInput, ListInvoicesInput, InvoiceList, ListInvoicesResult>(
                        InvoiceDirectory.newListInvoicesStep(
                                        "ListInvoices",
                                        ListInvoicesInput.class,
                                        new StepAnnotations(
                                                "invoices",
                                                "Invoices",
                                                "Read the first invoice page."),
                                        operation,
                                        new ConnectionRef("sage-intacct", "sage-intacct"),
                                        input -> input)
                                .branch("found", ReturnInvoiceList.class)
                                .build()) {};
    }

    @Override
    public String getFlowType() {
        return "JavaSageIntacctListInvoices";
    }

    @Override
    public StepList<ListInvoicesInput> getSteps() {
        return StepList.startStep(query).otherSteps(complete);
    }

    @Override
    public PersistenceSchema getPersistenceSchema() {
        return PersistenceSchema.of();
    }

    // dex:group group-id:invoices group-label:"Invoices"
    // dex:explanation text:"Return the invoice keys and IDs."
    public static final class ReturnInvoiceList implements Step<ListInvoicesResult> {
        @Override
        public Class<ListInvoicesResult> getInputType() {
            return ListInvoicesResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListInvoicesResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
