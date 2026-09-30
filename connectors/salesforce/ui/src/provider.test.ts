// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { isSalesforceAPIName, standardSObjects } from "./provider.js";

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
