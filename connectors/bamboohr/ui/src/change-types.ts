// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * changeTypes lists the values the employeeChanged Trigger accepts, in the order of the Go connector's
 * EmployeeChangeType constants, with blank first; a Go test keeps the two lists equal.
 */
export const changeTypes = Object.freeze([
  {changeType: "", label: "Every change", description: "Inserted, updated, and deleted employees"},
  {changeType: "inserted", label: "Inserted", description: "Employees added to BambooHR, such as new hires"},
  {changeType: "updated", label: "Updated", description: "Record, employment status, job information, or compensation changes, including terminations"},
  {changeType: "deleted", label: "Deleted", description: "Employee records deleted from BambooHR"},
] as const);

/** savedChangeType returns the saved change type when the Trigger accepts it, and "" otherwise. */
export function savedChangeType(value: unknown): string {
  return changeTypes.some(({changeType}) => changeType === value) ? value as string : "";
}
