// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/models.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newProjectConnection } from './project.js';
export { HelpScoutClient, type HelpScoutClientOptions, type HelpScoutWebhookSettings } from './client.js';
export { SearchConversationsOperation, GetConversationOperation, ReplyToConversationOperation, UpdateConversationOperation, FindCustomerByEmailOperation } from './operations.js';
export {
  ConversationEventEndpointRunner, ConversationEventSchema, ConversationEventTriggerConfigurationSchema, ConversationEventTriggerDefinition, HelpScoutSignatureInvalidError,
  UIMailboxPickerPortMailboxID, UIUnitMailboxPicker, WebhookEventAgentReplyCreated, WebhookEventConversationAssigned, WebhookEventConversationCreated,
  WebhookEventConversationCustomFieldsUpdated, WebhookEventConversationMerged, WebhookEventConversationMoved, WebhookEventConversationStatusUpdated,
  WebhookEventConversationTagsUpdated, WebhookEventCustomerReplyCreated, WebhookEventNoteCreated, conversationEventWebhookHandler, conversationWebhookEvents,
  defineConversationEventTriggerBinding, newConversationEventTrigger, newProjectConversationEventEndpointRunner, validateConversationEventTriggerConfiguration,
  verifyHelpScoutSignature,
  type ConversationEvent, type ConversationEventTriggerBindingConfig, type ConversationEventTriggerConfig, type ConversationEventTriggerConfiguration,
  type ProjectConversationEventTriggerRoute,
} from './trigger.js';
