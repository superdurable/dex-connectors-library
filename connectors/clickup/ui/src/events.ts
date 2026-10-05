// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * taskEvents lists the events the taskEvent Trigger decodes, in the order of the Go connector's
 * TaskEvents; a Go test keeps the two lists equal.
 */
export const taskEvents = Object.freeze([
  {event: "taskCreated", description: "A task was created"},
  {event: "taskUpdated", description: "A task changed"},
  {event: "taskDeleted", description: "A task was deleted"},
  {event: "taskPriorityUpdated", description: "The priority changed"},
  {event: "taskStatusUpdated", description: "The status changed"},
  {event: "taskAssigneeUpdated", description: "An assignee was added or removed"},
  {event: "taskDueDateUpdated", description: "The due date changed"},
  {event: "taskTagUpdated", description: "A tag was added or removed"},
  {event: "taskMoved", description: "The task moved to another List"},
  {event: "taskCommentPosted", description: "A comment was posted"},
  {event: "taskCommentUpdated", description: "A comment was edited"},
  {event: "taskTimeEstimateUpdated", description: "The time estimate changed"},
  {event: "taskTimeTrackedUpdated", description: "Tracked time changed"},
] as const);

/** selectedSupportedEvents keeps the saved events the Trigger supports, in the supported order. */
export function selectedSupportedEvents(value: unknown): string[] {
  const saved = Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
  return taskEvents.map(({event}) => event).filter((event) => saved.includes(event));
}
