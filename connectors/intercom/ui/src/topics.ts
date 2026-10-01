// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * conversationEventTopics lists the topics the conversationEvent Trigger decodes, in the order of the
 * Go connector's ConversationEventTopics; a Go test keeps the two lists equal.
 */
export const conversationEventTopics = Object.freeze([
  {topic: "conversation.user.created", description: "A customer started a conversation"},
  {topic: "conversation.user.replied", description: "The customer replied"},
  {topic: "conversation.admin.replied", description: "A teammate replied"},
  {topic: "conversation.admin.single.created", description: "A teammate started a one-to-one conversation"},
  {topic: "conversation.admin.assigned", description: "A teammate assigned the conversation"},
  {topic: "conversation.admin.open.assigned", description: "An open conversation was assigned"},
  {topic: "conversation.admin.noted", description: "A teammate added an internal note"},
  {topic: "conversation.admin.closed", description: "A teammate closed the conversation"},
  {topic: "conversation.admin.opened", description: "A teammate reopened the conversation"},
  {topic: "conversation.admin.snoozed", description: "A teammate snoozed the conversation"},
  {topic: "conversation.admin.unsnoozed", description: "A snoozed conversation reopened"},
  {topic: "conversation.operator.replied", description: "Fin or another bot replied"},
  {topic: "conversation.priority.updated", description: "The priority changed"},
  {topic: "conversation.rating.added", description: "The customer rated the conversation"},
] as const);

/** selectedSupportedTopics keeps the saved topics the Trigger supports, in the supported order. */
export function selectedSupportedTopics(value: unknown): string[] {
  const saved = Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
  return conversationEventTopics.map(({topic}) => topic).filter((topic) => saved.includes(topic));
}
