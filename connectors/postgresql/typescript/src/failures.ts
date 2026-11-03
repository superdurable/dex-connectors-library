// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Failure } from '@superdurable/connectors-sdk';

export type Phase = 'connect' | 'BEGIN' | 'the statement' | 'COMMIT';
export type Disposition = 'retry' | 'rejected' | 'uncertain' | 'invalidResponse' | 'defect';
export interface ClassifiedFailure { disposition: Disposition; kind: Failure['kind']; message: string; sqlState?: string }

const conditionNames: Record<string, string> = {
  '08000': 'connection_exception', '08003': 'connection_does_not_exist', '08006': 'connection_failure',
  '0A000': 'feature_not_supported', '22001': 'string_data_right_truncation', '22003': 'numeric_value_out_of_range',
  '22007': 'invalid_datetime_format', '22008': 'datetime_field_overflow', '22012': 'division_by_zero',
  '22021': 'character_not_in_repertoire', '22P02': 'invalid_text_representation', '23502': 'not_null_violation',
  '23503': 'foreign_key_violation', '23505': 'unique_violation', '23514': 'check_violation',
  '23P01': 'exclusion_violation', '25001': 'active_sql_transaction', '25006': 'read_only_sql_transaction',
  '28000': 'invalid_authorization_specification', '28P01': 'invalid_password', '3D000': 'invalid_catalog_name',
  '3F000': 'invalid_schema_name', '40001': 'serialization_failure', '40P01': 'deadlock_detected',
  '42501': 'insufficient_privilege', '42601': 'syntax_error', '42703': 'undefined_column',
  '42704': 'undefined_object', '42804': 'datatype_mismatch', '42883': 'undefined_function',
  '42P01': 'undefined_table', '42P18': 'indeterminate_datatype', '53100': 'disk_full', '53200': 'out_of_memory',
  '53300': 'too_many_connections', '55P03': 'lock_not_available', '57014': 'query_canceled',
  '57P01': 'admin_shutdown', '57P02': 'crash_shutdown', '57P03': 'cannot_connect_now',
};

function errorFields(error: unknown): Record<string, unknown> {
  return error !== null && typeof error === 'object' ? error as Record<string, unknown> : {};
}

export function classifyFailure(error: unknown, phase: Phase): ClassifiedFailure {
  const fields = errorFields(error);
  const code = typeof fields.code === 'string' ? fields.code : '';
  if (/^[A-Z0-9]{5}$/.test(code)) {
    const name = conditionNames[code];
    const description = name ? `${code} (${name})` : code;
    let disposition: Disposition = 'rejected';
    let kind: Failure['kind'] = 'PROVIDER_REJECTION';
    if (phase === 'COMMIT' && !code.startsWith('23') && !code.startsWith('40')) {
      return { disposition: 'uncertain', kind: 'AVAILABILITY', sqlState: code, message: `PostgreSQL returned SQLSTATE ${description} while committing, so whether the transaction committed is unknown` };
    }
    if (['40001', '40P01', '55P03'].includes(code)) { disposition = 'retry'; kind = 'CONFLICT'; }
    else if (['57P01', '57P02', '57P03'].includes(code) || code.startsWith('53') || code.startsWith('58')) { disposition = 'retry'; kind = 'AVAILABILITY'; }
    else if (code.startsWith('08')) { disposition = 'retry'; kind = 'TRANSPORT'; }
    else if (['28000', '28P01'].includes(code)) kind = 'AUTHENTICATION';
    else if (['42501', '25006'].includes(code)) kind = 'AUTHORIZATION';
    else if (['3D000', '3F000', '42P01', '42703', '42704', '42883'].includes(code)) kind = 'NOT_FOUND';
    else if (code.startsWith('23')) kind = 'CONFLICT';
    else if (code === '57014') kind = 'AVAILABILITY';
    else if (code.startsWith('22') || code.startsWith('42') || code.startsWith('0A')) kind = 'VALIDATION';
    let message = `PostgreSQL returned SQLSTATE ${description} during ${phase}`;
    if (code === '57014') message += '; the statement exceeded statementTimeout or was canceled';
    if (typeof fields.constraint === 'string' && /^[\x20-\x7e]{1,128}$/.test(fields.constraint)) message += ` on constraint ${JSON.stringify(fields.constraint)}`;
    return { disposition, kind, sqlState: code, message };
  }
  if (fields.name === 'UndecodableValueError') return { disposition: 'invalidResponse', kind: 'PROTOCOL', message: typeof fields.safeMessage === 'string' ? fields.safeMessage : 'PostgreSQL returned undecodable column text' };
  if (fields.name === 'StatementDescriptionError') return { disposition: 'defect', kind: 'VALIDATION', message: 'statement returns a column name more than once; give each result column a unique alias' };
  if (fields.name === 'ConnectionSettingsError') return { disposition: 'defect', kind: 'VALIDATION', message: 'PostgreSQL connection settings are invalid' };
  if (fields.name === 'CommitNotSentError') return { disposition: 'retry', kind: 'TRANSPORT', message: 'the connection ended or the call deadline passed before COMMIT was sent, so nothing was committed' };
  if (fields.name === 'CommitRolledBackError') return { disposition: 'rejected', kind: 'PROVIDER_REJECTION', message: 'PostgreSQL rolled back the transaction at COMMIT' };
  if (phase === 'connect') {
    if (['UNABLE_TO_VERIFY_LEAF_SIGNATURE', 'SELF_SIGNED_CERT_IN_CHAIN', 'DEPTH_ZERO_SELF_SIGNED_CERT', 'CERT_HAS_EXPIRED', 'ERR_TLS_CERT_ALTNAME_INVALID'].includes(code)) return { disposition: 'rejected', kind: 'AUTHENTICATION', message: 'the PostgreSQL server certificate failed sslMode verification' };
    if (code === 'ENOTFOUND' || code === 'EAI_NONAME') return { disposition: 'rejected', kind: 'NOT_FOUND', message: 'the PostgreSQL host name does not resolve' };
    if (fields.message === 'The server does not support SSL connections') return { disposition: 'rejected', kind: 'PROVIDER_REJECTION', message: 'the PostgreSQL server does not accept TLS, which sslMode requires' };
    return { disposition: 'retry', kind: 'AVAILABILITY', message: 'PostgreSQL is unreachable or did not finish connecting in time' };
  }
  if (phase === 'COMMIT') return { disposition: 'uncertain', kind: 'TRANSPORT', message: 'the connection failed after COMMIT was sent, so whether the transaction committed is unknown' };
  return { disposition: 'retry', kind: 'TRANSPORT', message: `the connection failed during ${phase} before anything was committed` };
}
