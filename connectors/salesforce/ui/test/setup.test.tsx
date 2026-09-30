// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { SalesforceSetupView } from "../src/setup.js";
import { SalesforceConfigurationUnit } from "../src/units.js";

function unitTarget(overrides: Partial<ConnectorStudioConfigurationUnitTarget> = {}): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit",
    scope: {kind: "operation", operationId: "queryRecords", flowType: "SalesforceRecordSync", stepType: "FindMatchingRecords"},
    instanceId: "recordObject",
    unitId: "sObjectPicker",
    label: "Record object",
    description: "Choose the object this Flow matches, links, and creates, such as Contact or Lead; every Salesforce Step in this Flow uses it, and blank means Contact.",
    required: false,
    bindings: [{port: "sObjectType", jsonPointer: "/sObjectType"}],
    value: {},
    ...overrides,
  };
}

function externalIDFieldTarget(overrides: Partial<ConnectorStudioConfigurationUnitTarget> = {}): ConnectorStudioConfigurationUnitTarget {
  return unitTarget({
    instanceId: "externalIdField", unitId: "fieldNameInput", label: "External ID field", required: true,
    description: "Enter the chosen object's field marked External ID and Unique, such as ERP_Id__c; the Flow stamps it on a matched record and creates records by it, so it cannot be blank.",
    bindings: [{port: "fieldName", jsonPointer: "/externalIdField"}],
    ...overrides,
  });
}

const noop = () => undefined;

describe("Salesforce setup", () => {
  it("shows the connected user without credential material", () => {
    const markup = renderToStaticMarkup(<SalesforceSetupView authMethodIds={["salesforce-oauth"]} connection={{state: "connected", accountEmail: "admin@example.com", grantedScopes: ["api", "refresh_token"]}} onConnect={noop} onReconnect={noop}/>);
    expect(markup).toContain("admin@example.com");
    expect(markup).not.toContain("access_token");
    expect(markup).not.toContain("refresh_token");
    expect(markup).not.toContain("instance_url");
  });

  it("offers OAuth connection and reconnection for the web server flow methods", () => {
    const connect = renderToStaticMarkup(<SalesforceSetupView authMethodIds={[]} connection={{state: "not_configured", grantedScopes: []}} onConnect={noop} onReconnect={noop}/>);
    expect(connect).toContain("Connect Salesforce");
    const reconnect = renderToStaticMarkup(<SalesforceSetupView authMethodIds={["salesforce-sandbox-oauth"]} connection={{state: "revoked", grantedScopes: []}} onConnect={noop} onReconnect={noop}/>);
    expect(reconnect).toContain("Connection revoked");
    expect(reconnect).toContain("Reconnect Salesforce");
  });

  it("never offers an OAuth button for the JWT bearer method", () => {
    const setup = renderToStaticMarkup(<SalesforceSetupView authMethodIds={["salesforce-jwt-bearer"]} connection={{state: "not_configured", grantedScopes: []}} onConnect={noop} onReconnect={noop}/>);
    expect(setup).toContain("Dex mints each session itself");
    expect(setup).not.toContain("Connect Salesforce");
    const expired = renderToStaticMarkup(<SalesforceSetupView authMethodIds={["salesforce-jwt-bearer"]} connection={{state: "expired", grantedScopes: []}} onConnect={noop} onReconnect={noop}/>);
    expect(expired).toContain("pre-authorized");
    expect(expired).not.toContain("Reconnect Salesforce");
  });

  it("renders every sObject picker field with operation guidance and blank semantics", () => {
    const markup = renderToStaticMarkup(<SalesforceConfigurationUnit onSave={noop} target={unitTarget({value: {sObjectType: "Contact"}})}/>);
    expect(markup).toContain('class="studio-surface"');
    expect(markup).toContain("Record object");
    expect(markup).toContain("blank means Contact.");
    expect(markup).toContain('<option value="">No object; the Flow decides</option>');
    expect(markup).toContain('<option value="Contact" selected="">Contact</option>');
    expect(markup).toContain("Setup &gt; Object Manager &gt; the object &gt; API Name");
    expect(markup).toContain('value="Contact"');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button">Save');
    expect(markup).not.toContain("<style");
  });

  it("accepts a custom object API name that the list does not show", () => {
    const markup = renderToStaticMarkup(<SalesforceConfigurationUnit onSave={noop} target={unitTarget({value: {sObjectType: "Invoice__c"}})}/>);
    expect(markup).toContain('<option value="" selected="">No object; the Flow decides</option>');
    expect(markup).toContain('value="Invoice__c"');
    expect(markup).not.toContain('role="alert"');
  });

  it("rejects an object value that is not an API name", () => {
    const markup = renderToStaticMarkup(<SalesforceConfigurationUnit onSave={noop} target={unitTarget({value: {sObjectType: "Contact WHERE Name != null"}})}/>);
    expect(markup).toContain('role="alert">Enter an object API name such as Contact or Invoice__c, without spaces.');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });

  it("renders the field name input and requires it only when the Flow marks it required", () => {
    const required = renderToStaticMarkup(<SalesforceConfigurationUnit onSave={noop} target={externalIDFieldTarget()}/>);
    expect(required).toContain("External ID field");
    expect(required).toContain("so it cannot be blank.");
    expect(required).toContain("Setup &gt; Object Manager &gt; the object &gt; Fields &amp; Relationships");
    expect(required).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
    const optional = renderToStaticMarkup(<SalesforceConfigurationUnit onSave={noop} target={externalIDFieldTarget({required: false})}/>);
    expect(optional).toContain('class="studio-button studio-button-primary" type="button">Save');
    const invalid = renderToStaticMarkup(<SalesforceConfigurationUnit onSave={noop} target={externalIDFieldTarget({value: {fieldName: "Account.Name"}})}/>);
    expect(invalid).toContain('role="alert">Enter a field API name such as Email or ERP_Id__c, without spaces or relationship dots.');
  });
});
