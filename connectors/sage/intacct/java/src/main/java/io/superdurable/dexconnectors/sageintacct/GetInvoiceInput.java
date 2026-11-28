// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sageintacct;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Select one Accounts Receivable invoice by its system key. */
public final class GetInvoiceInput {
    /** System-assigned Sage Intacct invoice key. */
    public final String invoiceKey;

    /** Construct the GetInvoiceInput wire value. Omitted text fields default to empty. */
    @JsonCreator
    public GetInvoiceInput(@JsonProperty("invoiceKey") String invoiceKey) {
        this.invoiceKey = invoiceKey == null ? "" : invoiceKey;
    }
}
