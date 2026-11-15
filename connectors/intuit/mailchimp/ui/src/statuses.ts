// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * memberStatuses lists the contact statuses the memberChanged Trigger filters on, in the order of the Go
 * connector's MemberStatuses; a Go test keeps the two lists equal.
 */
export const memberStatuses = Object.freeze([
  {status: "subscribed", description: "Subscribed: receives marketing campaigns"},
  {status: "unsubscribed", description: "Unsubscribed: opted out of marketing campaigns"},
  {status: "cleaned", description: "Cleaned: removed from sending after hard bounces"},
  {status: "pending", description: "Pending: sent a confirmation email, not confirmed yet"},
  {status: "transactional", description: "Transactional: receives only transactional email"},
  {status: "archived", description: "Archived: archived in the audience"},
] as const);

/** selectedSupportedStatuses keeps the saved statuses the Trigger supports, in the supported order. */
export function selectedSupportedStatuses(value: unknown): string[] {
  const saved = Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
  return memberStatuses.map(({status}) => status).filter((status) => saved.includes(status));
}

const audienceIdPattern = /^[A-Za-z0-9]{1,64}$/;

/** audienceIdProblem explains why an entered audience ID cannot be saved, or returns undefined. */
export function audienceIdProblem(audienceId: string): string | undefined {
  if (audienceId === "") return "Enter the audience ID.";
  if (!audienceIdPattern.test(audienceId)) return "An audience ID is 1 to 64 letters and digits, such as 57afe96172, with no spaces.";
  return undefined;
}
