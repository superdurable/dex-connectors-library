// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { CalendarSetupView } from "../src/setup.js";
import { CalendarConfigurationUnit } from "../src/units.js";

const pickerTarget: ConnectorStudioConfigurationUnitTarget = {
  kind: "configurationUnit", scope: {kind: "operation", operationId: "createEvent", flowType: "GoogleCalendarScheduleMeeting", stepType: "PlaceMeetingHold"},
  instanceId: "meetingCalendar", unitId: "calendarPicker", label: "Meeting calendar",
  description: "Choose the calendar that receives the meeting. Leave it unsaved to use the primary calendar.", required: false,
  bindings: [{port: "calendarId", jsonPointer: "/calendarId"}, {port: "calendarName", jsonPointer: "/calendarName"}],
  value: {calendarId: "team@group.calendar.google.com", calendarName: "Ops Team"},
};

describe("Google Calendar setup", () => {
  it("shows the connected account without any credential", () => {
    const markup = renderToStaticMarkup(<CalendarSetupView connection={{state: "connected", accountEmail: "owner@example.com", grantedScopes: [
      "https://www.googleapis.com/auth/calendar.events", "https://www.googleapis.com/auth/calendar.events.freebusy",
      "https://www.googleapis.com/auth/calendar.calendarlist.readonly",
    ]}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain("owner@example.com");
    expect(markup).not.toContain("Choose calendar");
    expect(markup).not.toContain("access_token");
    expect(markup).not.toContain("refresh_token");
  });

  it.each([
    ["revoked", "Connection revoked"],
    ["insufficient_scope", "Additional permission required"],
  ] as const)("shows a reconnect action for %s", (state, label) => {
    const markup = renderToStaticMarkup(<CalendarSetupView connection={{state, grantedScopes: []}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain(label);
    expect(markup).toContain("keep every requested scope checked");
    expect(markup).toContain("Reconnect Google Calendar");
  });

  it("renders the saved picker value with its operation-specific guidance", () => {
    const markup = renderToStaticMarkup(<CalendarConfigurationUnit onChooseCalendar={() => undefined} onSave={() => undefined} target={pickerTarget}/>);
    expect(markup).toContain('class="studio-surface"');
    expect(markup).toContain("Choose the calendar that receives the meeting. Leave it unsaved to use the primary calendar.");
    expect(markup).toContain("<strong>Calendar:</strong> Ops Team");
    expect(markup).toContain('value="team@group.calendar.google.com"');
    expect(markup).toContain('placeholder="primary"');
    expect(markup).toContain("Google Calendar &gt; Settings &gt; Integrate calendar");
    expect(markup).toContain('class="studio-button studio-button-primary" type="button">Save');
    expect(markup).not.toContain("more calendars than one list can show");
    expect(markup).not.toContain("<style");
  });

  it("lists calendars with role and zone, and points to manual entry when the list is truncated", () => {
    const markup = renderToStaticMarkup(<CalendarConfigurationUnit
      calendars={[{id: "owner@example.com", name: "owner@example.com", accessRole: "owner", timeZone: "America/Los_Angeles", isPrimary: true}]}
      isCalendarListTruncated
      onChooseCalendar={() => undefined} onSave={() => undefined} target={{...pickerTarget, value: {}}}/>);
    expect(markup).toContain("owner@example.com (primary, owner, America/Los_Angeles)");
    expect(markup).toContain('role="status">Google Calendar returned more calendars than one list can show. Enter a calendar ID to use one that is not listed.');
  });

  it("shows a load error inline and requires a calendar only for a required unit", () => {
    const markup = renderToStaticMarkup(<CalendarConfigurationUnit loadError="The caller does not have permission"
      onChooseCalendar={() => undefined} onSave={() => undefined} target={{...pickerTarget, required: true, value: {}}}/>);
    expect(markup).toContain('role="alert">The caller does not have permission');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });
});
