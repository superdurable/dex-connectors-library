// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { isCalendlyEventTypeURI, parseCalendlyCurrentUserURI, parseCalendlyEventTypePage } from "./provider.js";

describe("Calendly provider responses", () => {
  it("reads the connected user's URI from GET /users/me", () => {
    expect(parseCalendlyCurrentUserURI({resource: {uri: "https://api.calendly.com/users/USER0001", name: "Hana"}}))
      .toBe("https://api.calendly.com/users/USER0001");
    expect(() => parseCalendlyCurrentUserURI({resource: {uri: "https://evil.example/users/X"}})).toThrow("connected user");
  });

  it("keeps named event types and the next page token in connector-owned code", () => {
    expect(parseCalendlyEventTypePage({collection: [
      {uri: "https://api.calendly.com/event_types/TYPE0001", name: "30 Minute Meeting", duration: 30, active: true},
      {uri: "https://api.calendly.com/event_types/TYPE0002", name: "Retired", duration: 15, active: false},
      {uri: "https://api.calendly.com/users/USER0001", name: "Not an event type"},
      {name: "No URI"},
    ], pagination: {count: 4, next_page_token: "tok_2", next_page: "https://api.calendly.com/event_types?page_token=tok_2"}})).toEqual({
      eventTypes: [
        {uri: "https://api.calendly.com/event_types/TYPE0001", name: "30 Minute Meeting", durationMinutes: 30, isActive: true},
        {uri: "https://api.calendly.com/event_types/TYPE0002", name: "Retired", durationMinutes: 15, isActive: false},
      ],
      nextPageToken: "tok_2",
    });
    expect(parseCalendlyEventTypePage({collection: [], pagination: {next_page_token: null}})).toEqual({eventTypes: [], nextPageToken: ""});
  });

  it("reports an error object in connector words, never Calendly's text", () => {
    expect(() => parseCalendlyEventTypePage({title: "Unauthenticated", message: "secret detail"})).toThrow("Calendly returned an error instead of the expected data");
    expect(() => parseCalendlyEventTypePage({title: "Unauthenticated", message: "secret detail"})).not.toThrow("secret detail");
    expect(() => parseCalendlyEventTypePage({title: "Unauthenticated", message: "secret detail"})).not.toThrow("Unauthenticated");
  });

  it("accepts only event type URIs as a manual fallback", () => {
    expect(isCalendlyEventTypeURI("https://api.calendly.com/event_types/88323028-5ef5-448b-9776-bc0c71b062a4")).toBe(true);
    expect(isCalendlyEventTypeURI("https://calendly.com/acme/30min")).toBe(false);
    expect(isCalendlyEventTypeURI("https://api.calendly.com/event_types/")).toBe(false);
  });
});
