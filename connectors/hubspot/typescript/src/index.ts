// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/models.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newProjectConnection } from './project.js';
export { HubSpotClient, type HubSpotClientOptions } from './client.js';
export { SearchObjectsOperation, GetObjectOperation, UpsertObjectOperation, UpdateObjectOperation } from './operations.js';
export {
  CRMObjectChangeCreated, CRMObjectChangeUpdated, CRMObjectChangedEventSchema, CRMObjectChangedTriggerConfigurationSchema, CRMObjectChangedTriggerDefinition, MaximumTriggerProperties,
  UIObjectTypePickerPortObjectType, UIUnitObjectTypePicker, defineCRMObjectChangedTriggerBinding, validateCRMObjectChangedTriggerConfiguration,
  type CRMObjectChangeKind, type CRMObjectChangedEvent, type CRMObjectChangedTriggerBindingConfig, type CRMObjectChangedTriggerConfiguration,
} from './crm_object_changed.js';
export { CRMObjectChangeSearchError } from './crm_object_change_fetcher.js';
export {
  newCRMObjectChangedTrigger, newCRMObjectTriggerRunner, newProjectCRMObjectTriggerRunner,
  type CRMObjectChangedTriggerConfig, type CRMObjectChangedTriggerRoute, type CRMObjectTriggerRunnerConfig, type ProjectCRMObjectChangedTriggerRoute, type ProjectCRMObjectTriggerRunnerConfig,
} from './trigger.js';
