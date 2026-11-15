// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export { TwilioClient, type TwilioClientOptions, type TwilioRequestSender } from './client.js';
export { SendMessageOperation, GetMessageOperation } from './operations.js';
export { newConnection, newProjectConnection } from './project.js';
export { Connection, newConnectionWithOperations } from './generated/connection.js';
export { newSendMessageStep, newGetMessageStep } from './generated/steps.js';
export { SendMessageDefinition, GetMessageDefinition, SendMessageBranch, GetMessageBranch } from './generated/definitions.js';
export { ConfigSchema, SendMessageInputSchema, GetMessageInputSchema, MessageSchema, decodeCredentials } from './generated/models.js';
export type { Config, Credentials, SendMessageInput, GetMessageInput, Message } from './generated/models.js';
export { isE164PhoneNumber } from './client.js';
export {
  InboundMessageDirection, MessageStatusReceiving, MessageStatusReceived, UIUnitReceivingAddressInput, UIReceivingAddressInputPortTo,
  MessageReceivedTriggerConfigurationSchema, MessageReceivedEventSchema, MessageReceivedTriggerDefinition,
  defineMessageReceivedTriggerBinding, decodeMessageReceivedPayload, newMessageTriggerRunner, newProjectMessageTriggerRunner, newMessageReceivedTrigger,
  type MessageReceivedTriggerConfiguration, type MessageReceivedEvent, type MessageReceivedTriggerBindingConfig,
  type MessageReceivedTriggerRoute, type ProjectMessageReceivedTriggerRoute, type MessageReceivedTriggerConfig,
} from './trigger.js';
