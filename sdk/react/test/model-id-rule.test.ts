// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";

import modelIDCases from "../../../sdkgo/llm/llmtest/testdata/model_id_cases.json" with { type: "json" };
import { validateModelIDForRule, type ModelIDRule } from "../src/index.js";

const ruleNames: readonly ModelIDRule[] = ["body", "pathSegment"];

describe("validateModelIDForRule", () => {
  it("reads the fixture sdkgo's llm model ID rules are pinned by", () => {
    expect(modelIDCases.schemaVersion).toBe("connectors.dex.dev/model-id-cases/v1");
    expect(Object.keys(modelIDCases.rules).sort()).toEqual([...ruleNames].sort());
    for (const rule of ruleNames) expect(modelIDCases.cases.some((modelCase) => modelCase.rule === rule), rule).toBe(true);
  });

  it.each(modelIDCases.cases)("$rule: $name", (modelCase) => {
    expect(ruleNames).toContain(modelCase.rule);
    const validation = validateModelIDForRule(modelCase.rule as ModelIDRule, modelCase.input);
    if (modelCase.valid) {
      expect(validation).toEqual({isValid: true, modelId: modelCase.modelId});
      return;
    }
    expect(validation.isValid).toBe(false);
    if (!validation.isValid && modelCase.input.trim() !== "") expect(validation.message).not.toContain(modelCase.input.trim());
  });

  it("trims exactly the whitespace Go's strings.TrimSpace trims", () => {
    // Go trims U+0085 and keeps U+FEFF; String.prototype.trim does the opposite.
    expect(validateModelIDForRule("body", "\u0085gpt-6-sol\u0085")).toEqual({isValid: true, modelId: "gpt-6-sol"});
    expect(validateModelIDForRule("pathSegment", "\u3000models/gemini-3.5-flash\u2028")).toEqual({isValid: true, modelId: "gemini-3.5-flash"});
    expect(validateModelIDForRule("body", "\ufeffgpt-6-sol").isValid).toBe(false);
    expect(validateModelIDForRule("pathSegment", "\u200bgemini").isValid).toBe(false);
  });

  it("rejects a rule sdkgo does not define", () => {
    expect(validateModelIDForRule("query" as ModelIDRule, "gpt-6-sol")).toEqual({isValid: false, message: "The model ID rule is invalid."});
  });
});
