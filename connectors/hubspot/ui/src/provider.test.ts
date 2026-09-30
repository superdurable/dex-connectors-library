// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { isHubSpotOwnerID, parseHubSpotDealPipelines, parseHubSpotOwnerPage } from "./provider.js";

describe("HubSpot owner responses", () => {
  it("keeps active owners with numeric IDs and the after cursor", () => {
    expect(parseHubSpotOwnerPage({
      results: [
        {id: "77", firstName: "Ada", lastName: "Lovelace", email: "ada@example.com", type: "PERSON", archived: false},
        {id: "78", email: "support@example.com", type: "QUEUE", archived: false},
        {id: "79", firstName: "Former", archived: true},
        {id: "not-numeric", firstName: "Broken"},
      ],
      paging: {next: {after: "80", link: "https://api.hubapi.com/crm/owners/2026-09?after=80"}},
    })).toEqual({
      owners: [
        {id: "77", displayName: "Ada Lovelace", email: "ada@example.com", isQueue: false},
        {id: "78", displayName: "support@example.com", email: "support@example.com", isQueue: true},
      ],
      nextCursor: "80",
    });
  });

  it("ends pagination without a next page and names only the error category", () => {
    expect(parseHubSpotOwnerPage({results: []}).nextCursor).toBe("");
    expect(() => parseHubSpotOwnerPage({status: "error", message: "secret detail", category: "MISSING_SCOPES"}))
      .toThrow("HubSpot returned no owner list (MISSING_SCOPES)");
    expect(() => parseHubSpotOwnerPage({status: "error", message: "secret detail", category: "secret detail"}))
      .toThrow(/^HubSpot returned no owner list$/);
  });

  it("accepts blank or numeric owner IDs only", () => {
    expect(isHubSpotOwnerID("")).toBe(true);
    expect(isHubSpotOwnerID("123456789")).toBe(true);
    expect(isHubSpotOwnerID("12a")).toBe(false);
    expect(isHubSpotOwnerID(" 12")).toBe(false);
  });
});

describe("HubSpot deal pipeline responses", () => {
  it("keeps active pipelines and stages in display order", () => {
    expect(parseHubSpotDealPipelines({results: [
      {id: "partner", label: "Partner deals", displayOrder: 2, archived: false, stages: []},
      {id: "default", label: "Sales Pipeline", displayOrder: 0, archived: false, stages: [
        {id: "closedwon", label: "Closed won", displayOrder: 5, archived: false, metadata: {isClosed: "true", probability: "1.0"}},
        {id: "appointmentscheduled", label: "Appointment scheduled", displayOrder: 0, archived: false, metadata: {isClosed: "false"}},
        {id: "retired", label: "Retired", displayOrder: 1, archived: true},
      ]},
      {id: "archived", label: "Old pipeline", displayOrder: 1, archived: true, stages: []},
    ]})).toEqual([
      {id: "default", label: "Sales Pipeline", stages: [
        {id: "appointmentscheduled", label: "Appointment scheduled", isClosed: false},
        {id: "closedwon", label: "Closed won", isClosed: true},
      ]},
      {id: "partner", label: "Partner deals", stages: []},
    ]);
  });

  it("rejects a response without results", () => {
    expect(() => parseHubSpotDealPipelines({category: "MISSING_SCOPES"})).toThrow("HubSpot returned no deal pipelines (MISSING_SCOPES)");
  });
});
