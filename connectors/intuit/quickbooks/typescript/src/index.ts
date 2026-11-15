// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export { QuickBooksClient, type QuickBooksClientOptions } from './client.js';
export { newConnection, newConnectionWithOperations, newProjectCredentialProvider, Connection } from './generated/connection.js';
export * from './generated/models.js';
export * from './generated/steps.js';
export * from './generated/definitions.js';
export { newProjectConnection } from './project.js';
export {
  InvoiceChangeCreated, InvoiceChangeUpdated, InvoiceChangedEventSchema, InvoiceChangedTriggerConfigurationSchema, InvoiceChangedTriggerDefinition, defineInvoiceChangedTriggerBinding,
  type InvoiceChangeKind, type InvoiceChangedEvent, type InvoiceChangedTriggerBindingConfig, type InvoiceChangedTriggerConfiguration,
} from './trigger_definition.js';
export { InvoiceChangeCursorInvalidError, InvoiceChangeLookbackMs, MaximumSeenInvoiceVersions } from './invoice_change_cursor.js';
export { InvoiceChangeFetcher, InvoiceChangePageSize, invoiceChangeEventId, invoiceVersionKey } from './invoice_change_fetcher.js';
export {
  decodeInvoiceChangedEventPayload, newInvoiceChangedTrigger, newInvoiceTriggerRunner, newProjectInvoiceTriggerRunner, parseInvoiceChangedTriggerConfiguration,
  type InvoiceChangedTriggerConfig, type InvoiceChangedTriggerRoute, type InvoiceTriggerRunnerConfig, type ProjectInvoiceChangedTriggerRoute, type ProjectInvoiceTriggerRunnerConfig,
} from './trigger.js';
