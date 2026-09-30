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
