// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforcepollfake

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// answerQuery serves the Trigger's baseline and change statements; any other SOQL is MALFORMED_QUERY.
func (org *Org) answerQuery(statement string) (int, []byte) {
	if match := seedQueryPattern.FindStringSubmatch(statement); match != nil {
		horizon, err := time.Parse(time.RFC3339, match[2])
		if err != nil {
			return http.StatusBadRequest, errorBody("MALFORMED_QUERY")
		}
		matching := org.sortedRecords(match[1], func(record *storedRecord) bool { return !record.modstamp.After(horizon) })
		if len(matching) > 0 {
			matching = matching[len(matching)-1:]
		}
		return http.StatusOK, queryBody(matching, []string{"Id", "SystemModstamp"})
	}
	match := changesQueryPattern.FindStringSubmatch(statement)
	if match == nil {
		return http.StatusBadRequest, errorBody("MALFORMED_QUERY")
	}
	fields := strings.Split(match[1], ", ")
	horizon, horizonErr := time.Parse(time.RFC3339, match[3])
	afterText, afterID := match[4], ""
	if afterText == "" {
		afterText, afterID = match[5], match[7]
		if match[6] != match[5] {
			return http.StatusBadRequest, errorBody("MALFORMED_QUERY")
		}
	}
	after, afterErr := time.Parse(time.RFC3339, afterText)
	limit, limitErr := strconv.Atoi(match[8])
	if horizonErr != nil || afterErr != nil || limitErr != nil {
		return http.StatusBadRequest, errorBody("MALFORMED_QUERY")
	}
	matching := org.sortedRecords(match[2], func(record *storedRecord) bool {
		isAfter := record.modstamp.After(after) || (afterID != "" && record.modstamp.Equal(after) && record.id > afterID)
		return isAfter && !record.modstamp.After(horizon)
	})
	if len(matching) > limit {
		matching = matching[:limit]
	}
	return http.StatusOK, queryBody(matching, fields)
}

// answerRecordRead serves GET /sobjects/{type}/{id}?fields=a,b.
func (org *Org) answerRecordRead(path string, fieldList string) (int, []byte) {
	sObjectType, id, isFound := strings.Cut(path, "/")
	record := org.records[id]
	if !isFound || record == nil || record.sObjectType != sObjectType {
		return http.StatusNotFound, errorBody("NOT_FOUND")
	}
	encoded, err := json.Marshal(recordObject(record, strings.Split(fieldList, ",")))
	if err != nil {
		return http.StatusInternalServerError, errorBody("UNKNOWN_EXCEPTION")
	}
	return http.StatusOK, encoded
}

// sortedRecords returns the object's matching records in (SystemModstamp, Id) order.
func (org *Org) sortedRecords(sObjectType string, isMatching func(*storedRecord) bool) []*storedRecord {
	var matching []*storedRecord
	for _, record := range org.records {
		if record.sObjectType == sObjectType && isMatching(record) {
			matching = append(matching, record)
		}
	}
	sort.Slice(matching, func(left, right int) bool {
		if !matching[left].modstamp.Equal(matching[right].modstamp) {
			return matching[left].modstamp.Before(matching[right].modstamp)
		}
		return matching[left].id < matching[right].id
	})
	return matching
}

func queryBody(records []*storedRecord, fields []string) []byte {
	objects := make([]map[string]any, 0, len(records))
	for _, record := range records {
		objects = append(objects, recordObject(record, fields))
	}
	encoded, err := json.Marshal(map[string]any{"totalSize": len(objects), "done": true, "records": objects})
	if err != nil {
		panic(err) // Only JSON-safe test values reach the fake.
	}
	return encoded
}

// recordObject renders the requested fields; a field the record never set is null, as Salesforce returns it.
func recordObject(record *storedRecord, fields []string) map[string]any {
	object := map[string]any{"attributes": map[string]string{
		"type": record.sObjectType, "url": DataPathPrefix + "/sobjects/" + record.sObjectType + "/" + record.id,
	}}
	for _, field := range fields {
		switch field {
		case "Id":
			object[field] = record.id
		case "CreatedDate":
			object[field] = record.created.Format(dateTimeLayout)
		case "LastModifiedDate":
			object[field] = record.modified.Format(dateTimeLayout)
		case "SystemModstamp":
			object[field] = record.modstamp.Format(dateTimeLayout)
		default:
			object[field] = record.fields[field]
		}
	}
	return object
}

func errorBody(errorCode string) []byte {
	return []byte(`[{"message":"fake Salesforce error","errorCode":"` + errorCode + `","fields":[]}]`)
}

func copyFields(fields map[string]any) map[string]any {
	copied := make(map[string]any, len(fields))
	for name, value := range fields {
		copied[name] = value
	}
	return copied
}
