// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/models.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newProjectConnection } from './project.js';
export { DocsClient, type DocsClientOptions, type DocsTransport } from './client.js';
export { GetDocumentTextOperation, CreateDocumentOperation, ReplaceDocumentTextOperation, AppendTextOperation } from './operations.js';
