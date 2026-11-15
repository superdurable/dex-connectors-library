// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

const phoneNumberPattern = /^\+[1-9][0-9]{6,14}$/;

/**
 * isReceivingAddress accepts what the Go messageReceived configuration accepts: an E.164 number such as
 * +14155550100 or a WhatsApp sender written whatsapp:+14155550100, without surrounding spaces.
 */
export function isReceivingAddress(value: string): boolean {
  const phoneNumber = value.startsWith("whatsapp:") ? value.slice("whatsapp:".length) : value;
  return phoneNumberPattern.test(phoneNumber);
}
