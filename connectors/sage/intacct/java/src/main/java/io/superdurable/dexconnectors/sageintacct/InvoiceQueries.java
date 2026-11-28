// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sageintacct;

import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.cfg.*;
import com.fasterxml.jackson.databind.type.LogicalType;

import java.io.IOException;
import java.net.*;
import java.util.*;
import java.util.regex.Pattern;

final class InvoiceQueries {
    private static final Pattern INVOICE_KEY = Pattern.compile("[A-Za-z0-9_-]{1,128}");
    private static final ObjectMapper WIRE =
            SageIntacctClient.JSON
                    .copy()
                    .enable(MapperFeature.ACCEPT_CASE_INSENSITIVE_PROPERTIES)
                    .disable(
                            DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES,
                            DeserializationFeature.ACCEPT_FLOAT_AS_INT);

    static {
        WIRE.coercionConfigFor(LogicalType.Integer)
                .setCoercion(CoercionInputShape.String, CoercionAction.Fail)
                .setCoercion(CoercionInputShape.Boolean, CoercionAction.Fail);
        MutableCoercionConfig strings = WIRE.coercionConfigFor(LogicalType.Textual);
        for (CoercionInputShape shape :
                Arrays.asList(
                        CoercionInputShape.Integer,
                        CoercionInputShape.Float,
                        CoercionInputShape.Boolean))
            strings.setCoercion(shape, CoercionAction.Fail);
    }

    private InvoiceQueries() {}

    static final class Get extends SageIntacctClient.InvoiceQuery<GetInvoiceInput, InvoiceSummary> {
        Get(SageIntacctClient client) {
            super(client, "getInvoice", new InvoiceSummary("", ""));
        }

        String successBranch() {
            return "found";
        }

        String suffix(GetInvoiceInput input) {
            if (input == null || !INVOICE_KEY.matcher(input.invoiceKey).matches())
                throw new IllegalArgumentException("Invalid invoice key");
            return "/" + input.invoiceKey;
        }

        InvoiceSummary decode(byte[] body, GetInvoiceInput input) throws IOException {
            WireInvoice envelope = WIRE.readValue(body, WireInvoice.class);
            InvoiceSummary invoice = summary(envelope == null ? null : envelope.result);
            if (!input.invoiceKey.equals(invoice.key))
                throw new IOException("Mismatched invoice key");
            return invoice;
        }
    }

    static final class ListPage
            extends SageIntacctClient.InvoiceQuery<ListInvoicesInput, InvoiceList> {
        ListPage(SageIntacctClient client) {
            super(client, "listInvoices", new InvoiceList("", Collections.emptyList()));
        }

        String successBranch() {
            return "found";
        }

        String suffix(ListInvoicesInput input) {
            return "";
        }

        InvoiceList decode(byte[] body, ListInvoicesInput input) throws IOException {
            WirePage page = WIRE.readValue(body, WirePage.class);
            if (page == null || page.result == null) throw new IOException("Invalid invoice page");
            List<InvoiceSummary> invoices = new ArrayList<>(page.result.size());
            for (InvoiceSummary invoice : page.result) invoices.add(summary(invoice));
            return new InvoiceList(page.metadata == null ? "" : page.metadata.next, invoices);
        }
    }

    private static InvoiceSummary summary(InvoiceSummary invoice) throws IOException {
        if (invoice == null || !INVOICE_KEY.matcher(invoice.key).matches() || invoice.id.isEmpty())
            throw new IOException("Incomplete invoice identity");
        return invoice;
    }

    private static final class WireInvoice {
        @JsonProperty("ia::result")
        public InvoiceSummary result;
    }

    private static final class WirePage {
        @JsonProperty("ia::result")
        public List<InvoiceSummary> result;

        @JsonProperty("ia::meta")
        public WireMetadata metadata;
    }

    private static final class WireMetadata {
        public String next;
    }
}
