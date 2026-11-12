// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { categoryIconNames } from "./categoryIcons.mjs";

// The manifest schema enum is pinned to schema/connector_categories.go by a Go test.
function taxonomyCategoryIDs() {
  const manifestSchema = JSON.parse(readFileSync(new URL("../../schema/connector-manifest.schema.json", import.meta.url), "utf8"));
  return manifestSchema.properties.metadata.properties.categories.items.enum;
}

function declaredIconNames() {
  const source = readFileSync(new URL("./Icons.tsx", import.meta.url), "utf8");
  const shapes = source.slice(source.indexOf("const iconShapes = {"), source.indexOf("} satisfies Record"));
  return new Set([...shapes.matchAll(/^ {2}([A-Za-z]+): /gm)].map((match) => match[1]));
}

test("every taxonomy category has an icon, and no icon names a removed category", () => {
  assert.deepEqual(Object.keys(categoryIconNames).sort(), [...taxonomyCategoryIDs()].sort());
});

test("every category icon names a shape in the icon set", () => {
  const iconNames = declaredIconNames();
  assert.ok(iconNames.size > 0);
  for (const [categoryID, iconName] of Object.entries(categoryIconNames)) {
    assert.ok(iconNames.has(iconName), `${categoryID} uses missing icon ${iconName}`);
  }
});
