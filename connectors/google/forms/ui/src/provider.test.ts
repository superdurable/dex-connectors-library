// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { formIDFromInput, isResponderLink, parseFormPage } from "./provider.js";

describe("Google Forms provider responses", () => {
  it("keeps the form file identity and pagination in connector-owned code", () => {
    expect(parseFormPage({files: [{id: "1FAIpQLintake", name: "Vendor intake"}], nextPageToken: "next"})).toEqual({
      forms: [{id: "1FAIpQLintake", title: "Vendor intake"}], nextPageToken: "next",
    });
  });

  it("drops files without a usable ID or name", () => {
    expect(parseFormPage({files: [{id: "bad id", name: "Spaces"}, {id: "form-2"}, "not a record", {id: "form-3", name: "Survey"}]})).toEqual({
      forms: [{id: "form-3", title: "Survey"}], nextPageToken: "",
    });
    expect(parseFormPage({})).toEqual({forms: [], nextPageToken: ""});
  });

  it.each([
    ["1FAIpQL_intake-2", "1FAIpQL_intake-2"],
    ["  1FAIpQLintake  ", "1FAIpQLintake"],
    ["https://docs.google.com/forms/d/1FAIpQLintake/edit", "1FAIpQLintake"],
    ["https://docs.google.com/forms/u/1/d/1FAIpQLintake/edit#responses", "1FAIpQLintake"],
    ["https://docs.google.com/forms/d/1FAIpQLintake", "1FAIpQLintake"],
    ["https://docs.google.com/forms/d/e/1FAIpQLSresponder/viewform", ""],
    ["https://forms.gle/AbCdEf", ""],
    ["not a form", ""],
    ["", ""],
  ])("reads the form ID from %j", (input, expected) => {
    expect(formIDFromInput(input)).toBe(expected);
  });

  it("recognizes a responder link so the unit can explain it", () => {
    expect(isResponderLink("https://docs.google.com/forms/d/e/1FAIpQLSresponder/viewform?usp=sf_link")).toBe(true);
    expect(isResponderLink("https://docs.google.com/forms/d/1FAIpQLintake/edit")).toBe(false);
  });
});
