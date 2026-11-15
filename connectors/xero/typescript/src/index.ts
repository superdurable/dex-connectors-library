// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/models.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newProjectConnection } from './project.js';
export { XeroClient, type XeroClientOptions } from './client.js';
export { ListContactsOperation, FindContactByEmailOperation, GetInvoiceOperation, ListInvoicesOperation, CreateInvoiceOperation, RecordPaymentOperation } from './operations.js';
export {
  InvoiceChangedEventSchema, InvoiceChangedTriggerConfigurationSchema, InvoiceChangedTriggerDefinition, InvoiceStatuses, InvoiceTypes,
  UIInvoiceChangeFilterPortStatuses, UIInvoiceChangeFilterPortType, UIUnitInvoiceChangeFilter, defineInvoiceChangedTriggerBinding,
  type InvoiceChangedEvent, type InvoiceChangedTriggerBindingConfig, type InvoiceChangedTriggerConfiguration,
} from './trigger_definition.js';
export { InvoiceChangeCursorInvalidError, InvoiceChangeOverlapMs, MaximumSeenInvoiceChanges } from './invoice_change_cursor.js';
export {
  InvoiceChangeFetcher, InvoiceChangePageSize, MaximumInvoiceChangeWindowPages, invoiceChangeEventId, invoiceChangeFilterKey, seenInvoiceChangeKey,
  validateInvoiceChangedTriggerConfiguration,
} from './invoice_change_fetcher.js';
export {
  decodeInvoiceChangedEventPayload, newInvoiceChangedTrigger, newInvoiceTriggerRunner, newProjectInvoiceTriggerRunner, parseInvoiceChangedTriggerConfiguration,
  type InvoiceChangedTriggerConfig, type InvoiceChangedTriggerRoute, type InvoiceTriggerRunnerConfig, type ProjectInvoiceChangedTriggerRoute, type ProjectInvoiceTriggerRunnerConfig,
} from './trigger.js';
