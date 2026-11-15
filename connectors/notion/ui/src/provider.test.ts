// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { listDatabaseDataSources, parseNotionID, readDataSource } from "./provider.js";

const databaseID = "2a4d6f80-1b3c-4d5e-8f70-112233445500";
const dataSourceID = "2a4d6f80-1b3c-4d5e-8f70-112233445566";

describe("Notion provider responses", () => {
  it("parses IDs and database links like the Go connector", () => {
    expect(parseNotionID("2A4D6F801B3C4D5E8F70112233445500")).toBe(databaseID);
    expect(parseNotionID(` ${databaseID} `)).toBe(databaseID);
    expect(parseNotionID("https://www.notion.so/acme/Requests-2a4d6f801b3c4d5e8f70112233445500?v=abc")).toBe(databaseID);
    expect(parseNotionID(`https://app.notion.com/p/${databaseID}`)).toBe(databaseID);
    expect(parseNotionID("https://evil.example/2a4d6f801b3c4d5e8f70112233445500")).toBe("");
    expect(parseNotionID("not-an-id")).toBe("");
  });

  it("lists a database's data sources through the getDatabase command", async () => {
    const calls: string[] = [];
    const listed = await listDatabaseDataSources(async (commandId, parameters) => {
      calls.push(`${commandId}:${parameters.databaseId}`);
      return {object: "database", id: databaseID, data_sources: [
        {id: dataSourceID, name: "Requests"},
        {id: "2a4d6f80-1b3c-4d5e-8f70-112233445567", name: ""},
        {id: "../evil", name: "Not an ID"},
      ]};
    }, "https://www.notion.so/Requests-2a4d6f801b3c4d5e8f70112233445500");
    expect(calls).toEqual([`getDatabase:${databaseID}`]);
    expect(listed).toEqual([
      {id: dataSourceID, name: "Requests"},
      {id: "2a4d6f80-1b3c-4d5e-8f70-112233445567", name: "2a4d6f80-1b3c-4d5e-8f70-112233445567"},
    ]);
  });

  it("reads a pasted data source's title through the getDataSource command", async () => {
    const checked = await readDataSource(async (commandId, parameters) => {
      expect(commandId).toBe("getDataSource");
      expect(parameters).toEqual({dataSourceId: dataSourceID});
      return {object: "data_source", id: dataSourceID, title: [{plain_text: "Requests"}, {plain_text: " 2026"}]};
    }, dataSourceID.replaceAll("-", ""));
    expect(checked).toEqual({id: dataSourceID, name: "Requests 2026"});
  });

  it("reports errors in connector words, never Notion's message", async () => {
    const notFound = async () => ({object: "error", status: 404, code: "object_not_found", message: "secret detail"});
    await expect(listDatabaseDataSources(notFound, databaseID)).rejects.toThrow("share the database with this connection");
    await expect(readDataSource(notFound, dataSourceID)).rejects.not.toThrow("secret detail");
    await expect(listDatabaseDataSources(notFound, "nope")).rejects.toThrow("Enter a Notion database link");
  });
});
