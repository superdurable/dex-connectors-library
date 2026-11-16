// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/models.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newProjectConnection } from './project.js';
export { BambooHRClient, MinimumPollIntervalMs, type BambooHRClientOptions, type BambooHRRequestSender } from './client.js';
export { AddEmployeeOperation, FindEmployeeByEmailOperation, GetEmployeeOperation, ListEmployeeChangesOperation, ListTimeOffRequestsOperation, UpdateEmployeeOperation, buildTimeOffRequestFilter } from './operations.js';
export {
  EmployeeChangeActionDeleted, EmployeeChangeActionInserted, EmployeeChangeActionUpdated, EmployeeChangeSettleWindowMs, EmployeeChangeTypes,
  employeeChangedEventId, validateEmployeeChangedTriggerConfiguration, type EmployeeChangedEvent, type EmployeeChangedTriggerConfiguration,
} from './employee_change_fetcher.js';
export {
  EmployeeChangedEventSchema, EmployeeChangedTriggerConfigurationSchema, EmployeeChangedTriggerDefinition, UIChangeTypePickerPortChangeType, UIUnitChangeTypePicker,
  decodeEmployeeChangedEventPayload, defineEmployeeChangedTriggerBinding, newEmployeeChangedTrigger, newEmployeeChangedTriggerRunner, newProjectEmployeeChangedTriggerRunner,
  type EmployeeChangedTriggerBindingConfig, type EmployeeChangedTriggerConfig, type EmployeeChangedTriggerRoute, type EmployeeChangedTriggerRunnerOptions, type ProjectEmployeeChangedTriggerRoute,
} from './trigger.js';
