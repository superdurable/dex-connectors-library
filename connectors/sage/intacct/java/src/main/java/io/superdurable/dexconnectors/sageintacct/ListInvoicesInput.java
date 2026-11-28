// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sageintacct;

import com.fasterxml.jackson.annotation.JsonValue;

import java.util.Collections;
import java.util.Map;

/** Request the first page of Accounts Receivable invoices. */
public final class ListInvoicesInput {
    @JsonValue
    private Map<String, Object> fields() {
        return Collections.emptyMap();
    }
}
