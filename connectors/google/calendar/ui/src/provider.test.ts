// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { minimumAccessRole, parseCalendarPage } from "./provider.js";

function pickerTarget(operationId: string): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit", scope: {kind: "operation", operationId, flowType: "MeetingFlow", stepType: "PlaceHold"},
    instanceId: "calendar", unitId: "calendarPicker", label: "Calendar", required: false,
    bindings: [{port: "calendarId", jsonPointer: "/calendarId"}], value: {},
  };
}

describe("Google Calendar provider responses", () => {
  it("keeps calendar identity, display name, access role, zone, and pagination in connector-owned code", () => {
    expect(parseCalendarPage({
      nextPageToken: "next",
      items: [
        {id: "owner@example.com", summary: "owner@example.com", primary: true, accessRole: "owner", timeZone: "America/Los_Angeles"},
        {id: "team@group.calendar.google.com", summary: "Team", summaryOverride: "Ops Team", accessRole: "writer", timeZone: "America/New_York"},
        {id: "holidays@group.v.calendar.google.com", accessRole: "reader"},
        {id: "unknown-role@example.com", summary: "Ignored", accessRole: "none"},
        {summary: "Missing ID", accessRole: "owner"},
      ],
    })).toEqual({
      nextPageToken: "next",
      calendars: [
        {id: "owner@example.com", name: "owner@example.com", accessRole: "owner", timeZone: "America/Los_Angeles", isPrimary: true},
        {id: "team@group.calendar.google.com", name: "Ops Team", accessRole: "writer", timeZone: "America/New_York", isPrimary: false},
        {id: "holidays@group.v.calendar.google.com", name: "holidays@group.v.calendar.google.com", accessRole: "reader", timeZone: "", isPrimary: false},
      ],
    });
    expect(parseCalendarPage({})).toEqual({calendars: [], nextPageToken: ""});
  });

  it("lists only calendars an operation can use", () => {
    expect(minimumAccessRole(pickerTarget("createEvent"))).toBe("writer");
    expect(minimumAccessRole(pickerTarget("updateEvent"))).toBe("writer");
    expect(minimumAccessRole(pickerTarget("listEvents"))).toBe("reader");
    expect(minimumAccessRole(pickerTarget("getEvent"))).toBe("reader");
    expect(minimumAccessRole(pickerTarget("queryFreeBusy"))).toBe("freeBusyReader");
  });
});
