// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { CalendarSetupView } from "../src/setup.js";
import { CalendarConfigurationUnit } from "../src/units.js";

const pickerTarget: ConnectorStudioConfigurationUnitTarget = {
  kind: "configurationUnit", scope: {kind: "operation", operationId: "createEvent", flowType: "OutlookCalendarBookMeeting", stepType: "PlaceMeetingHold"},
  instanceId: "meetingCalendar", unitId: "calendarPicker", label: "Meeting calendar",
  description: "Choose the calendar that receives the meeting. Leave it unsaved to use the mailbox's default calendar.", required: false,
  bindings: [{port: "calendarId", jsonPointer: "/calendarId"}, {port: "calendarName", jsonPointer: "/calendarName"}],
  value: {calendarId: "AAMkTeamCalendar", calendarName: "Ops Team"},
};

describe("Outlook Calendar setup", () => {
  it("shows the connected account without any credential", () => {
    const markup = renderToStaticMarkup(<CalendarSetupView authMethodIds={["microsoft-oauth"]}
      connection={{state: "connected", accountEmail: "owner@contoso.com", grantedScopes: ["offline_access", "Calendars.ReadWrite"]}}
      onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain("owner@contoso.com");
    expect(markup).not.toContain("Choose calendar");
    expect(markup).not.toContain("access_token");
    expect(markup).not.toContain("refresh_token");
  });

  it("offers Connect for delegated OAuth and form guidance for app-only", () => {
    const delegated = renderToStaticMarkup(<CalendarSetupView authMethodIds={["microsoft-oauth"]} connection={{state: "not_configured", grantedScopes: []}}
      onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(delegated).toContain("Connect Microsoft Outlook Calendar");
    const appOnly = renderToStaticMarkup(<CalendarSetupView authMethodIds={["app-only"]} connection={{state: "not_configured", grantedScopes: []}}
      onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(appOnly).not.toContain("Connect Microsoft Outlook Calendar");
    expect(appOnly).toContain("tenant ID, and mailbox");
    expect(appOnly).toContain("leave access_token blank");
  });

  it.each([
    ["revoked", "Connection revoked"],
    ["insufficient_scope", "Additional permission required"],
  ] as const)("shows a reconnect action for %s", (state, label) => {
    const markup = renderToStaticMarkup(<CalendarSetupView authMethodIds={["microsoft-oauth"]} connection={{state, grantedScopes: []}}
      onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain(label);
    expect(markup).toContain("accept every requested permission");
    expect(markup).toContain("Reconnect Microsoft Outlook Calendar");
  });

  it("renders the saved picker value with its operation-specific guidance", () => {
    const markup = renderToStaticMarkup(<CalendarConfigurationUnit onChooseCalendar={() => undefined} onSave={() => undefined} target={pickerTarget}/>);
    expect(markup).toContain('class="studio-surface"');
    expect(markup).toContain("Choose the calendar that receives the meeting. Leave it unsaved to use the mailbox&#x27;s default calendar.");
    expect(markup).toContain("<strong>Calendar:</strong> Ops Team");
    expect(markup).toContain('value="AAMkTeamCalendar"');
    expect(markup).toContain('placeholder="Default calendar"');
    expect(markup).toContain("Microsoft Graph GET /me/calendars");
    expect(markup).toContain('class="studio-button studio-button-primary" type="button">Save');
    expect(markup).not.toContain("more calendars than one list can show");
    expect(markup).not.toContain("<style");
  });

  it("lists calendars with access and owner, and points to manual entry when the list is truncated", () => {
    const markup = renderToStaticMarkup(<CalendarConfigurationUnit
      calendars={[{id: "AAMkCalendar1", name: "Calendar", canEdit: true, isDefault: true, ownerAddress: "owner@contoso.com"}]}
      isCalendarListTruncated
      onChooseCalendar={() => undefined} onSave={() => undefined} target={{...pickerTarget, value: {}}}/>);
    expect(markup).toContain("Calendar (default, can edit, owner@contoso.com)");
    expect(markup).toContain('role="status">Microsoft Graph returned more calendars than one list can show. Enter a calendar ID to use one that is not listed.');
  });

  it("shows a load error inline and requires a calendar only for a required unit", () => {
    const markup = renderToStaticMarkup(<CalendarConfigurationUnit loadError="Save the mailbox in the connection form before choosing a calendar."
      onChooseCalendar={() => undefined} onSave={() => undefined} target={{...pickerTarget, required: true, value: {}}}/>);
    expect(markup).toContain('role="alert">Save the mailbox in the connection form before choosing a calendar.');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });
});
