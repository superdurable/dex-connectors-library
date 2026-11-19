// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { validateLLMModelID } from "./model-selection.js";

/** maxConnectionModels is the connector's limit on the models field. */
export const maxConnectionModels = 100;

/** maxContextWindowTokens is the largest modelContextWindowTokens value the connector accepts. */
export const maxContextWindowTokens = 100_000_000;

/** readSettingLines splits a textarea into trimmed entries, ignoring blank lines. */
export function readSettingLines(text: string): string[] {
  return text.split("\n").map((line) => line.trim()).filter((line) => line !== "");
}

/** readSavedStringList returns a saved stringList field as textarea text. */
export function readSavedStringList(value: unknown): string {
  return Array.isArray(value) ? value.filter((entry): entry is string => typeof entry === "string").join("\n") : "";
}

/**
 * validateModelListSettings applies the connector's models and
 * modelContextWindowTokens rules to the form's entries. connectionModel is
 * the model the connection runs when a Step picks none. It returns a message
 * for the first problem, or undefined to accept the entries.
 */
export function validateModelListSettings(models: string[], contextWindows: string[], connectionModel: string): string | undefined {
  if (models.length > maxConnectionModels) return `List at most ${maxConnectionModels} offered models.`;
  const listed = new Set<string>();
  for (const [index, model] of models.entries()) {
    const problem = validateLLMModelID(model);
    if (problem !== undefined) return `Offered model ${index + 1}: ${problem}`;
    if (listed.has(model)) return `Offered model ${index + 1} repeats an earlier entry.`;
    listed.add(model);
  }
  if (listed.size > 0 && !listed.has(connectionModel)) {
    return "Add the connection's model, or the provider default when Model is blank, to the offered models.";
  }
  const windowModels = new Set<string>();
  for (const [index, entry] of contextWindows.entries()) {
    const separator = entry.lastIndexOf("=");
    const model = separator < 0 ? "" : entry.slice(0, separator).trim();
    const tokens = separator < 0 ? "" : entry.slice(separator + 1).trim();
    if (separator < 0 || model === "") return `Context window ${index + 1}: write it as <model ID>=<tokens>.`;
    if (!/^[0-9]{1,9}$/.test(tokens) || Number(tokens) < 1 || Number(tokens) > maxContextWindowTokens) {
      return `Context window ${index + 1}: tokens must be a whole number from 1 to ${maxContextWindowTokens}.`;
    }
    if (!listed.has(model)) return `Context window ${index + 1} names a model that is not an offered model.`;
    if (windowModels.has(model)) return `Context window ${index + 1} repeats an earlier model.`;
    windowModels.add(model);
  }
  return undefined;
}
