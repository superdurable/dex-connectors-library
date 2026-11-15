// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { XeroSetupView } from "../src/setup.js";
import { XeroConfigurationUnit } from "../src/units.js";

const sentinelSecret = "SENTINEL-XERO-CLIENT-SECRET";
const scope = {kind: "trigger" as const, triggerName: "invoiceChanged", bindingName: "invoice-changes", flowType: "XeroInvoiceChange"};
const bindings = [{port: "type", jsonPointer: "/type"}, {port: "statuses", jsonPointer: "/statuses"}];

describe("Xero setup", () => {
  it("keeps secrets in the host form and explains each connection state", () => {
    const methods = ["custom-connection", "xero-oauth"];
    const notConfigured = renderToStaticMarkup(<XeroSetupView connection={{state: "not_configured", grantedScopes: [], authMethodIds: methods}}/>);
    expect(notConfigured).toContain("https://developer.xero.com/app/manage");
    expect(notConfigured).toContain("never sends them to this panel");
    const connected = renderToStaticMarkup(<XeroSetupView connection={{state: "connected", grantedScopes: [], authMethodIds: methods}}/>);
    expect(connected).toContain("poll interval");
    const revoked = renderToStaticMarkup(<XeroSetupView connection={{state: "revoked", grantedScopes: [], authMethodIds: methods}}/>);
    expect(revoked).toContain("Generate a new secret");
    for (const markup of [notConfigured, connected, revoked]) expect(markup).not.toContain(sentinelSecret);
  });

  it("renders the invoice filter for the Trigger binding with its description, choices, and saved values", () => {
    const description = "Choose which invoices start this Flow; leave it unsaved to receive every invoice and bill.";
    const markup = renderToStaticMarkup(<XeroConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "invoiceFilter", unitId: "invoiceChangeFilter", label: "Invoices that start this Flow",
        description, required: false, bindings, value: {type: "ACCPAY", statuses: ["PAID"]}}}
      onSave={() => undefined}/>);
    expect(markup).toContain(description);
    for (const text of ["Sales invoices and bills", "Sales invoices (ACCREC)", "Bills (ACCPAY)", "https://go.xero.com", "check none to receive every status"]) {
      expect(markup).toContain(text);
    }
    for (const status of ["DRAFT", "SUBMITTED", "AUTHORISED", "PAID", "VOIDED", "DELETED"]) expect(markup).toContain(status);
    expect(markup).toMatch(/<option value="ACCPAY" selected="">/);
    expect(markup.match(/checked=""/g)).toHaveLength(1);
    expect(markup).not.toContain("Every invoice and bill created or changed starts the Flow");
    expect(markup).not.toContain(sentinelSecret);
  });

  it("says that a blank filter receives everything, and rejects an unknown unit", () => {
    const blank = renderToStaticMarkup(<XeroConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "invoiceFilter", unitId: "invoiceChangeFilter", label: "Invoices", required: false, bindings, value: {}}}
      onSave={() => undefined}/>);
    expect(blank).toContain("Every invoice and bill created or changed starts the Flow");
    const unknown = renderToStaticMarkup(<XeroConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "x", unitId: "contactPicker", label: "X", required: false, bindings: [], value: {}}}
      onSave={() => undefined}/>);
    expect(unknown).toContain("Unsupported Xero configuration unit: contactPicker");
  });
});
