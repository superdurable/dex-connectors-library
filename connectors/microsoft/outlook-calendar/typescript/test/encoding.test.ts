// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
import { expect, it } from "vitest";
import { graphQuery, pathEscape } from "../src/http.js";
import { instant, returnedBoundary } from "../src/event.js";
it.each([
  ["a b", "a=a%20b&z=a%20b", "a%20b"],
  ["x~*!'()", "a=x~%2A%21%27%28%29&z=x~%2A%21%27%28%29", "x~%2A%21%27%28%29"],
  [
    "calendar/id?=x+y",
    "a=calendar%2Fid%3F%3Dx%2By&z=calendar%2Fid%3F%3Dx%2By",
    "calendar%2Fid%3F=x+y",
  ],
  ["é", "a=%C3%A9&z=%C3%A9", "%C3%A9"],
])("matches Go query and path escaping for %s", (value, query, path) => {
  expect(graphQuery({ z: value, a: value })).toBe(query);
  expect(pathEscape(value)).toBe(path);
});
it.each([
  "2026-02-24T24:00:00Z",
  "2026-02-24T17:00:00",
  "2026-02-24T17:00:00.1234567Z",
])("rejects non-round-trippable time %s", (value) =>
  expect(() => instant("start.dateTime", value)).toThrow(/start.dateTime/),
);
it("accepts zero seventh fractional digit", () =>
  expect(
    returnedBoundary(
      "start",
      { dateTime: "2026-02-24T17:00:00.0000000", timeZone: "UTC" },
      "UTC",
    ).value.dateTime,
  ).toBe("2026-02-24T17:00:00Z"));
