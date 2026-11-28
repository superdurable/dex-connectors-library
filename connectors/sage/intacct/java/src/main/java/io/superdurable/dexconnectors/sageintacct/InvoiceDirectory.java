// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sageintacct;

import com.fasterxml.jackson.annotation.*;

import io.superdurable.dexconnectors.sdk.*;

import java.util.function.Function;

/** Operation-specific Dex Step factories for SageIntacct. */
public final class InvoiceDirectory {
    private InvoiceDirectory() {}

    /** Concrete Dex Step input for a GetInvoice result. */
    public static final class GetInvoiceResult extends QueryResult<InvoiceSummary> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public GetInvoiceResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") InvoiceSummary value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the GetInvoice query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<I, GetInvoiceInput, InvoiceSummary, GetInvoiceResult>
                    newGetInvoiceStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<GetInvoiceInput, InvoiceSummary> operation,
                            ConnectionRef connection,
                            Function<I, GetInvoiceInput> mapInput) {
        if (!"sage-intacct".equals(connection.getProvider())) {
            throw new IllegalArgumentException("SageIntacct connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                GetInvoiceResult::new);
    }

    /** Concrete Dex Step input for a ListInvoices result. */
    public static final class ListInvoicesResult extends QueryResult<InvoiceList> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListInvoicesResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") InvoiceList value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the ListInvoices query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<I, ListInvoicesInput, InvoiceList, ListInvoicesResult>
                    newListInvoicesStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<ListInvoicesInput, InvoiceList> operation,
                            ConnectionRef connection,
                            Function<I, ListInvoicesInput> mapInput) {
        if (!"sage-intacct".equals(connection.getProvider())) {
            throw new IllegalArgumentException("SageIntacct connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ListInvoicesResult::new);
    }
}
