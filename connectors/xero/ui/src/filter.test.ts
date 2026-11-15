// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { readInvoiceChangeFilter, savedInvoiceChangeFilter } from "./filter.js";

describe("Xero invoice change filter", () => {
  it("reads only Xero's own type and status values", () => {
    expect(readInvoiceChangeFilter({})).toEqual({type: "", statuses: []});
    expect(readInvoiceChangeFilter({type: "ACCPAY", statuses: ["PAID", "OPEN", "DRAFT"]})).toEqual({type: "ACCPAY", statuses: ["DRAFT", "PAID"]});
    expect(readInvoiceChangeFilter({type: "SALES", statuses: "PAID"})).toEqual({type: "", statuses: []});
  });

  it("saves blank choices as absent, so the Trigger receives every invoice and bill", () => {
    expect(savedInvoiceChangeFilter({type: "", statuses: []})).toEqual({});
    expect(savedInvoiceChangeFilter({type: "ACCREC", statuses: ["PAID", "AUTHORISED"]})).toEqual({type: "ACCREC", statuses: ["AUTHORISED", "PAID"]});
  });
});
