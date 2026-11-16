// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/models.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newProjectConnection } from './project.js';
export { ConfluenceClient, type ConfluenceClientOptions } from './client.js';
export { SearchPagesOperation, GetPageOperation, CreatePageOperation, UpdatePageOperation, AddCommentOperation } from './operations.js';
export {
  PageChangeCreated, PageChangeUpdated, PageChangedEventSchema, PageChangedTriggerConfigurationSchema, PageChangedTriggerDefinition,
  UISpacePickerPortSpaceID, UISpacePickerPortSpaceKey, UISpacePickerPortSpaceName, UIUnitSpacePicker, definePageChangedTriggerBinding,
  type PageChangeKind, type PageChangedEvent, type PageChangedTriggerBindingConfig, type PageChangedTriggerConfiguration,
} from './page_changed.js';
export { pageChangeEventId } from './page_change_cursor.js';
export {
  PageTriggerRunner, decodePageChangedEventPayload, newPageChangedTrigger, newPageTriggerRunner, newProjectPageTriggerRunner,
  type PageChangedTriggerConfig, type PageChangedTriggerRoute, type PageTriggerRunnerConfig, type ProjectPageChangedTriggerRoute,
} from './trigger.js';
