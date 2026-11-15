// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export interface StandardSObject { apiName: string; label: string; }

/** standardSObjects is a fixed list: Studio commands cannot reach each org's own instance URL. */
export const standardSObjects: readonly StandardSObject[] = [
  {apiName: "Account", label: "Account"},
  {apiName: "Contact", label: "Contact"},
  {apiName: "Lead", label: "Lead"},
  {apiName: "Opportunity", label: "Opportunity"},
  {apiName: "Case", label: "Case"},
  {apiName: "Task", label: "Task"},
  {apiName: "Campaign", label: "Campaign"},
  {apiName: "CampaignMember", label: "Campaign Member"},
];

const apiNamePattern = /^[A-Za-z][A-Za-z0-9_]{0,254}$/;

/** isSalesforceAPIName mirrors the connector's object and field API name rule. */
export function isSalesforceAPIName(value: string): boolean {
  return apiNamePattern.test(value) && !value.endsWith("_");
}

/** maxTriggerFieldNames mirrors salesforce.MaxTriggerFieldNames. */
export const maxTriggerFieldNames = 50;

/**
 * fieldNamesFromInput splits a comma- or newline-separated list into trimmed field API names. It reports
 * the first entry that is not an API name, a repeated name ignoring case, or a list over the limit.
 */
export function fieldNamesFromInput(value: string): {fieldNames: string[]; problem?: string} {
  const fieldNames = value.split(/[\s,]+/).map((name) => name.trim()).filter((name) => name !== "");
  const invalid = fieldNames.find((name) => !isSalesforceAPIName(name));
  if (invalid !== undefined) return {fieldNames, problem: `${invalid} is not a field API name such as StageName or ERP_Id__c.`};
  const seen = new Set<string>();
  for (const name of fieldNames) {
    if (seen.has(name.toLowerCase())) return {fieldNames, problem: `${name} is listed twice.`};
    seen.add(name.toLowerCase());
  }
  if (fieldNames.length > maxTriggerFieldNames) return {fieldNames, problem: `Enter at most ${maxTriggerFieldNames} field API names.`};
  return {fieldNames};
}
