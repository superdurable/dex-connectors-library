// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { fieldNamesFromInput, isSalesforceAPIName, maxTriggerFieldNames, standardSObjects } from "./provider.js";

describe("Salesforce API names", () => {
  it.each([
    ["Contact", true],
    ["Invoice__c", true],
    ["ns__Invoice__c", true],
    ["ERP_Id__c", true],
    ["", false],
    ["Contact WHERE Name != null", false],
    ["Account.Name", false],
    ["1Contact", false],
    ["Trailing_", false],
    ["Contact'", false],
  ])("accepts %j: %s", (value, expected) => {
    expect(isSalesforceAPIName(value)).toBe(expected);
  });

  it("lists only valid, unique standard objects", () => {
    const apiNames = standardSObjects.map((object) => object.apiName);
    expect(new Set(apiNames).size).toBe(apiNames.length);
    expect(apiNames.every(isSalesforceAPIName)).toBe(true);
    expect(apiNames).toContain("Contact");
  });
});

describe("Trigger field name lists", () => {
  it("splits commas and line breaks and drops blanks", () => {
    expect(fieldNamesFromInput(" Name, StageName\nERP_Id__c,, ")).toEqual({fieldNames: ["Name", "StageName", "ERP_Id__c"]});
    expect(fieldNamesFromInput("")).toEqual({fieldNames: []});
  });

  it("reports invalid, repeated, and too many names", () => {
    expect(fieldNamesFromInput("Name, Account.Name").problem).toContain("Account.Name is not a field API name");
    expect(fieldNamesFromInput("Email, email").problem).toBe("email is listed twice.");
    const tooMany = Array.from({length: maxTriggerFieldNames + 1}, (_, index) => `Field${index}__c`).join(",");
    expect(fieldNamesFromInput(tooMany).problem).toBe(`Enter at most ${maxTriggerFieldNames} field API names.`);
  });
});
