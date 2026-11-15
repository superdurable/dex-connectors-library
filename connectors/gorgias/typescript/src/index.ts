// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/models.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newProjectConnection } from './project.js';
export { GorgiasClient, type GorgiasClientOptions, type RequestSender } from './client.js';
export { SearchTicketsOperation, GetTicketOperation, FindCustomerByEmailOperation, CreateTicketOperation, UpdateTicketOperation, AddNoteOperation } from './operations.js';
export {
  TicketChangedEventSchema, TicketChangedTriggerConfigurationSchema, TicketChangedTriggerDefinition, TicketEventTypes, defineTicketChangedTriggerBinding, deliversTicketEventType, isTicketEventType,
  type TicketChangedEvent, type TicketChangedTriggerBindingConfig, type TicketChangedTriggerConfiguration, type TicketEventType,
} from './trigger_definition.js';
export { MaximumSeenEventKeys, TicketEventCursorInvalidError, TicketEventOverlapNanoseconds } from './ticket_event_cursor.js';
export { TicketEventFetcher, TicketEventPageSize, ticketEventKey } from './ticket_event_fetcher.js';
export {
  decodeTicketChangedEventPayload, newProjectTicketTriggerRunner, newTicketChangedTrigger, newTicketTriggerRunner, parseTicketChangedTriggerConfiguration,
  type ProjectTicketChangedTriggerRoute, type ProjectTicketTriggerRunnerConfig, type TicketChangedTriggerConfig, type TicketChangedTriggerRoute, type TicketTriggerRunnerConfig,
} from './trigger.js';
