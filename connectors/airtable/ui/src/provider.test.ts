// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { parseAirtableBasePage, parseAirtableTables } from "./provider.js";

describe("Airtable metadata responses", () => {
  it("keeps reachable bases and the offset cursor from one base page", () => {
    expect(parseAirtableBasePage({
      bases: [
        {id: "appRefundBase0001", name: "Refunds", permissionLevel: "create"},
        {id: "appHiddenBase0001", name: "Hidden", permissionLevel: "none"},
        {id: "not-a-base", name: "Broken", permissionLevel: "edit"},
        {id: "appNoNameBase0001", permissionLevel: "edit"},
      ],
      offset: "itr23sEjsdfEr3282/appSW9R5uCNmRmfl6",
    })).toEqual({
      bases: [{id: "appRefundBase0001", name: "Refunds", permissionLevel: "create"}],
      nextOffset: "itr23sEjsdfEr3282/appSW9R5uCNmRmfl6",
    });
    expect(parseAirtableBasePage({bases: []}).nextOffset).toBe("");
  });

  it("projects only table IDs and names from the base schema", () => {
    expect(parseAirtableTables({tables: [
      {id: "tblPolicies000001", name: "Policies", primaryFieldId: "fldPolicyKey00001", fields: [{id: "fldPolicyKey00001", name: "Policy Key", type: "singleLineText"}], views: []},
      {id: "tblDecisionLog001", name: "Decision Log", fields: [], views: []},
      {id: "viwNotATable00001", name: "Grid view"},
    ]})).toEqual([
      {id: "tblPolicies000001", name: "Policies", lastModifiedTimeFields: []},
      {id: "tblDecisionLog001", name: "Decision Log", lastModifiedTimeFields: []},
    ]);
  });

  it("lists only Last modified time fields that include a time of day", () => {
    const [table] = parseAirtableTables({tables: [{id: "tblIntakeLeads001", name: "Leads", fields: [
      {id: "fldLastModified01", name: "Last Modified", type: "lastModifiedTime", options: {isValid: true, referencedFieldIds: null, result: {type: "dateTime"}}},
      {id: "fldStatusChange01", name: "Status Changed", type: "lastModifiedTime"},
      {id: "fldModifiedDay001", name: "Modified Day", type: "lastModifiedTime", options: {result: {type: "date"}}},
      {id: "fldCreatedTime001", name: "Created", type: "createdTime", options: {result: {type: "dateTime"}}},
      {id: "not-a-field", name: "Broken", type: "lastModifiedTime"},
    ]}]});
    expect(table.lastModifiedTimeFields).toEqual([
      {id: "fldLastModified01", name: "Last Modified"},
      {id: "fldStatusChange01", name: "Status Changed"},
    ]);
  });

  it("names only Airtable's error type when a list is missing", () => {
    expect(() => parseAirtableBasePage({error: {type: "INVALID_PERMISSIONS_OR_MODEL_NOT_FOUND", message: "SENTINEL provider message text"}}))
      .toThrow("Airtable returned no base list (INVALID_PERMISSIONS_OR_MODEL_NOT_FOUND)");
    expect(() => parseAirtableTables({error: "SENTINEL provider message text"})).toThrow(/^Airtable returned no table list$/);
  });
});
