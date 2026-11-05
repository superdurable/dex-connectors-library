// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen

import "github.com/superdurable/dex-connectors-library/internal/codegen/gomodel"

func modelTypeContainsTime(modelType gomodel.Type) bool {
	if modelType.Kind == gomodel.Time {
		return true
	}
	if modelType.Elem != nil {
		return modelTypeContainsTime(*modelType.Elem)
	}
	return false
}

const goTimeJSONValidator = `const goTimeJSONPattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{1,2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|([+-])(\d{2}):(\d{2}))$/;

/** Match the timestamp strings accepted by Go time.Time.UnmarshalJSON. */
function isGoTimeJSON(value: string): boolean {
  const match = goTimeJSONPattern.exec(value);
  if (!match) return false;
  const [, yearText, monthText, dayText, hourText, minuteText, secondText, , zone, , zoneHourText, zoneMinuteText] = match;
  const year = Number(yearText);
  const month = Number(monthText);
  const day = Number(dayText);
  const isLeap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const monthDays = [31, isLeap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return month >= 1 && month <= 12 && day >= 1 && day <= monthDays[month - 1]!
    && Number(hourText) <= 23 && Number(minuteText) <= 59 && Number(secondText) <= 59
    && (zone === 'Z' || (Number(zoneHourText) <= 24 && Number(zoneMinuteText) <= 60));
}

/** Reproduce Go time.Time.MarshalJSON for timestamps it can marshal. */
function canonicalGoTimeJSON(value: string): string {
  const match = goTimeJSONPattern.exec(value)!;
  const [, year, month, day, hour, minute, second, fraction, zone, sign, zoneHour, zoneMinute] = match;
  const clock = year + '-' + month + '-' + day + 'T' + hour!.padStart(2, '0') + ':' + minute + ':' + second;
  const digits = (fraction ?? '').slice(0, 9).replace(/0+$/, '');
  const offsetMinutes = zone === 'Z' ? 0 : (Number(zoneHour) * 60 + Number(zoneMinute)) * (sign === '+' ? 1 : -1);
  if (Math.abs(offsetMinutes) >= 1440) return value;
  const offset = offsetMinutes === 0 ? 'Z' : (offsetMinutes > 0 ? '+' : '-')
    + String(Math.floor(Math.abs(offsetMinutes) / 60)).padStart(2, '0') + ':'
    + String(Math.abs(offsetMinutes) % 60).padStart(2, '0');
  return clock + (digits ? '.' + digits : '') + offset;
}

/** Go time.Time JSON input schema, including Go's accepted legacy spellings. */
const GoTimeJSONSchema = z.string().refine(isGoTimeJSON, 'invalid Go time.Time JSON timestamp').transform(canonicalGoTimeJSON);

`
