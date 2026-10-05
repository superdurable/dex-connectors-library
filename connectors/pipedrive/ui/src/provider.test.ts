// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import {
  isPipedriveCustomFieldKey,
  isPipedriveID,
  joinPipelineStages,
  parsePipedriveFieldPage,
  parsePipedrivePipelinePage,
  parsePipedriveStagePage,
  parsePipedriveUsers,
} from "./provider.js";

// Keys are built at run time, so no token-shaped 40-character literal is checked in.
const customFieldKey = "ab".repeat(20);
const tierFieldKey = "cd".repeat(20);

describe("Pipedrive user responses", () => {
  it("keeps active users with numeric IDs", () => {
    expect(parsePipedriveUsers({success: true, data: [
      {id: 7, name: "Ada Lovelace", email: "ada@example.com", active_flag: true},
      {id: 8, name: "", email: "bob@example.com", active_flag: true},
      {id: 9, name: "Former", active_flag: false},
      {id: "x", name: "Broken"},
    ]})).toEqual([
      {id: "7", name: "Ada Lovelace", email: "ada@example.com"},
      {id: "8", name: "bob@example.com", email: "bob@example.com"},
    ]);
  });

  it("names only Pipedrive's numeric error code, never its message text", () => {
    expect(() => parsePipedriveUsers({success: false, error: "secret detail", errorCode: 401})).toThrow(/^Pipedrive returned no user list \(HTTP 401\)$/);
    expect(() => parsePipedriveUsers({success: false, error: "secret detail", errorCode: "secret detail"})).toThrow(/^Pipedrive returned no user list$/);
  });
});

describe("Pipedrive pipeline and stage responses", () => {
  it("joins stages under their pipelines in order_nr order and keeps the cursor", () => {
    const pipelines = parsePipedrivePipelinePage({success: true, data: [
      {id: 2, name: "Partners", order_nr: 2, is_deleted: false},
      {id: 1, name: "Sales", order_nr: 1, is_deleted: false},
      {id: 3, name: "Old", order_nr: 0, is_deleted: true},
    ], additional_data: {next_cursor: "bmV4dA"}});
    expect(pipelines.nextCursor).toBe("bmV4dA");
    const stages = parsePipedriveStagePage({success: true, data: [
      {id: 4, name: "Negotiation", pipeline_id: 1, order_nr: 3, is_deleted: false},
      {id: 3, name: "Qualified", pipeline_id: 1, order_nr: 1, is_deleted: false},
      {id: 9, name: "Retired", pipeline_id: 1, order_nr: 2, is_deleted: true},
      {id: 8, name: "Intro", pipeline_id: 2, order_nr: 1},
    ], additional_data: {next_cursor: null}});
    expect(stages.nextCursor).toBe("");
    expect(joinPipelineStages(pipelines.items, stages.items)).toEqual([
      {id: "1", name: "Sales", stages: [{id: "3", name: "Qualified"}, {id: "4", name: "Negotiation"}]},
      {id: "2", name: "Partners", stages: [{id: "8", name: "Intro"}]},
    ]);
  });

  it("rejects a response without a data array", () => {
    expect(() => parsePipedriveStagePage({success: true, data: {}})).toThrow("Pipedrive returned no stages");
  });
});

describe("Pipedrive field responses", () => {
  it("keeps only custom fields with 40-character keys and marks text fields", () => {
    expect(parsePipedriveFieldPage({success: true, data: [
      {field_code: "title", field_name: "Title", field_type: "varchar", is_custom_field: false},
      {field_code: customFieldKey, field_name: "Lead source", field_type: "varchar", is_custom_field: true},
      {field_code: tierFieldKey, field_name: "Tier", field_type: "enum", is_custom_field: true},
      {field_code: "short", field_name: "Broken", field_type: "varchar", is_custom_field: true},
    ]}).items).toEqual([
      {key: customFieldKey, name: "Lead source", fieldType: "varchar", isText: true},
      {key: tierFieldKey, name: "Tier", fieldType: "enum", isText: false},
    ]);
  });

  it("accepts blank or well-formed IDs and keys only", () => {
    expect(isPipedriveID("")).toBe(true);
    expect(isPipedriveID("17")).toBe(true);
    expect(isPipedriveID("017")).toBe(false);
    expect(isPipedriveID("1a")).toBe(false);
    expect(isPipedriveCustomFieldKey(customFieldKey)).toBe(true);
    expect(isPipedriveCustomFieldKey("Lead source")).toBe(false);
  });
});
