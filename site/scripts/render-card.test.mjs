import assert from "node:assert/strict";
import test from "node:test";
import { companyCountLine } from "./render-card.mjs";

test("company line counts connectors, operations, and triggers", () => {
  const line = companyCountLine({
    companyDirectory: "google",
    company: "Google",
    connectors: [
      { operations: [{}, {}, {}], triggers: [{}] },
      { operations: [{}], triggers: [] },
    ],
  });
  assert.equal(line, "2 connectors · 4 operations · 1 trigger");
});
