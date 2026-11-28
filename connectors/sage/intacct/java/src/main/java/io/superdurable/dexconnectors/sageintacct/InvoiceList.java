// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sageintacct;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.*;

/** One page of invoice identities. */
public final class InvoiceList {
    /** Opaque link to the next page, or empty on the last page. */
    public final String next;

    /** Invoices on this page in provider order. */
    public final List<InvoiceSummary> invoices;

    /** Restore the value from its Dex wire fields. */
    @JsonCreator
    public InvoiceList(
            @JsonProperty("next") String next,
            @JsonProperty("invoices") List<InvoiceSummary> invoices) {
        this.next = next == null ? "" : next;
        this.invoices =
                invoices == null
                        ? Collections.emptyList()
                        : Collections.unmodifiableList(new ArrayList<>(invoices));
    }
}
