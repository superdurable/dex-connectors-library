// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { parseCalendarPage, selectCalendarListCommand, shouldListOnlyEditableCalendars } from "./provider.js";

function pickerTarget(operationId: string): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit", scope: {kind: "operation", operationId, flowType: "MeetingFlow", stepType: "PlaceHold"},
    instanceId: "calendar", unitId: "calendarPicker", label: "Calendar", required: false,
    bindings: [{port: "calendarId", jsonPointer: "/calendarId"}], value: {},
  };
}

describe("Outlook Calendar provider responses", () => {
  it("keeps calendar identity, name, edit access, and owner in connector-owned code", () => {
    expect(parseCalendarPage({
      "@odata.nextLink": "https://graph.microsoft.com/v1.0/me/calendars?$top=100&$skip=100",
      value: [
        {id: "AAMkCalendar1", name: "Calendar", canEdit: true, isDefaultCalendar: true, owner: {name: "Owner", address: "owner@contoso.com"}},
        {id: "AAMkCalendar2", name: "Team", canEdit: false, owner: {address: "team@contoso.com"}},
        {id: "AAMkCalendar3"},
        {name: "Missing ID", canEdit: true},
      ],
    })).toEqual({
      isTruncated: true,
      calendars: [
        {id: "AAMkCalendar1", name: "Calendar", canEdit: true, isDefault: true, ownerAddress: "owner@contoso.com"},
        {id: "AAMkCalendar2", name: "Team", canEdit: false, isDefault: false, ownerAddress: "team@contoso.com"},
        {id: "AAMkCalendar3", name: "AAMkCalendar3", canEdit: false, isDefault: false, ownerAddress: ""},
      ],
    });
    expect(parseCalendarPage({})).toEqual({calendars: [], isTruncated: false});
  });

  it("lists only editable calendars for an operation that writes events", () => {
    expect(shouldListOnlyEditableCalendars(pickerTarget("createEvent"))).toBe(true);
    expect(shouldListOnlyEditableCalendars(pickerTarget("updateEvent"))).toBe(true);
    expect(shouldListOnlyEditableCalendars(pickerTarget("listEvents"))).toBe(false);
    expect(shouldListOnlyEditableCalendars(pickerTarget("getEvent"))).toBe(false);
  });

  it("lists the signed-in user's calendars, or the app-only mailbox's calendars", () => {
    expect(selectCalendarListCommand({authMethodIds: ["microsoft-oauth"], configuration: {}})).toEqual({commandId: "listCalendars", parameters: {}});
    expect(selectCalendarListCommand({authMethodIds: ["app-only"], configuration: {mailbox: "scheduling@contoso.com"}}))
      .toEqual({commandId: "listMailboxCalendars", parameters: {mailbox: "scheduling@contoso.com"}});
    expect(selectCalendarListCommand({authMethodIds: [], configuration: {mailbox: "scheduling@contoso.com"}}))
      .toEqual({commandId: "listMailboxCalendars", parameters: {mailbox: "scheduling@contoso.com"}});
    expect(selectCalendarListCommand({authMethodIds: ["app-only"], configuration: {}}))
      .toEqual({error: "Save the mailbox in the connection form before choosing a calendar."});
  });
});
