// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fakecrm

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// DealStages are the fake organization's Deals Stage picklist values, Zoho CRM's default stages.
var DealStages = []string{
	"Qualification", "Needs Analysis", "Value Proposition", "Identify Decision Makers", "Proposal/Price Quote",
	"Negotiation/Review", "Closed Won", "Closed Lost", "Closed Lost to Competition",
}

type writeRequest struct {
	Data                 []map[string]any `json:"data"`
	DuplicateCheckFields []string         `json:"duplicate_check_fields"`
	Trigger              *[]string        `json:"trigger"`
}

// upsert applies the record first and answers afterwards, so a slow answer leaves the record committed.
func (server *Server) upsert(response http.ResponseWriter, request *http.Request, body []byte, module string) {
	attempt := server.record("upsert", request, body, "")
	var input writeRequest
	if json.Unmarshal(body, &input) != nil || len(input.Data) != 1 || len(input.DuplicateCheckFields) == 0 {
		server.writeJSON(response, http.StatusBadRequest, `{"code":"INVALID_DATA","details":{},"message":"SENTINEL","status":"error"}`)
		return
	}
	fields := input.Data[0]
	if _, isRejected := fields[server.RejectsUpsertField]; isRejected {
		server.writeValue(response, http.StatusBadRequest, recordError("INVALID_DATA", server.RejectsUpsertField))
		return
	}
	server.mutex.Lock()
	existing, duplicateField := server.findDuplicate(module, fields, input.DuplicateCheckFields)
	if existing == nil && module == "Contacts" && fields["Last_Name"] == nil {
		server.mutex.Unlock()
		server.writeValue(response, http.StatusBadRequest, recordError("MANDATORY_NOT_FOUND", "Last_Name"))
		return
	}
	if rejected := server.rejectedStage(module, fields); rejected != nil {
		server.mutex.Unlock()
		server.writeValue(response, http.StatusBadRequest, rejected)
		return
	}
	status, action := http.StatusOK, "update"
	if existing == nil {
		existing, status, action = server.storeRecord(module, map[string]any{}, server.advanceClock()), http.StatusCreated, "insert"
	} else {
		existing["Modified_Time"] = formatZohoTime(server.advanceClock())
	}
	server.applyFields(existing, fields)
	result := writeSuccess(action, existing, duplicateField)
	server.mutex.Unlock()
	if attempt == 1 && server.DelaysFirstUpsertOf == module {
		server.sleepBeforeAnswering()
	}
	server.writeValue(response, status, result)
}

func (server *Server) update(response http.ResponseWriter, request *http.Request, body []byte, module string, recordID string) {
	attempt := server.record("update", request, body, "")
	var input writeRequest
	if json.Unmarshal(body, &input) != nil || len(input.Data) != 1 {
		server.writeJSON(response, http.StatusBadRequest, `{"code":"INVALID_DATA","details":{},"message":"SENTINEL","status":"error"}`)
		return
	}
	server.mutex.Lock()
	existing, isFound := server.records[module][recordID]
	var rejected map[string]any
	switch {
	case server.LocksUpdates:
		rejected = recordError("RECORD_LOCKED", "id")
	case !isFound:
		rejected = recordError("INVALID_DATA", "id")
	default:
		rejected = server.rejectedStage(module, input.Data[0])
	}
	if rejected != nil {
		server.mutex.Unlock()
		server.writeValue(response, http.StatusBadRequest, rejected)
		return
	}
	existing["Modified_Time"] = formatZohoTime(server.advanceClock())
	server.applyFields(existing, input.Data[0])
	result := writeSuccess("", existing, nil)
	server.mutex.Unlock()
	if attempt == 1 && server.DelaysFirstUpdate {
		server.sleepBeforeAnswering()
	}
	if attempt == 1 && server.LosesFirstUpdateResponse {
		dropConnection(response)
		return
	}
	server.writeValue(response, http.StatusOK, result)
}

func (server *Server) getRecord(response http.ResponseWriter, request *http.Request, body []byte, module string, recordID string) {
	server.record("get", request, body, "")
	server.mutex.Lock()
	record, isFound := server.records[module][recordID]
	var returned map[string]any
	if isFound {
		returned = map[string]any{"$editable": true, "$approval": map[string]any{"approve": false}}
		fields := request.URL.Query().Get("fields")
		for key, value := range record {
			if fields == "" || key == "id" || slices.Contains(strings.Split(fields, ","), key) {
				returned[key] = value
			}
		}
	}
	server.mutex.Unlock()
	if !isFound {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	server.writeValue(response, http.StatusOK, map[string]any{"data": []any{returned}})
}

func (server *Server) listFields(response http.ResponseWriter, request *http.Request, body []byte) {
	server.record("fields", request, body, "")
	fields := []any{
		map[string]any{"api_name": "Owner", "display_label": "Owner", "data_type": "ownerlookup", "unique": map[string]any{}},
		map[string]any{"api_name": "Modified_Time", "display_label": "Modified Time", "data_type": "datetime", "read_only": true, "unique": map[string]any{}},
	}
	if request.URL.Query().Get("module") == "Deals" {
		var stages []any
		for _, stage := range DealStages {
			stages = append(stages, map[string]any{"display_value": stage, "actual_value": stage, "type": "used", "id": stage})
		}
		stages = append(stages, map[string]any{"display_value": "Retired", "actual_value": "Retired", "type": "unused", "id": "retired"})
		fields = append(fields,
			map[string]any{"api_name": "Deal_Name", "display_label": "Deal Name", "data_type": "text", "system_mandatory": true, "length": 120, "unique": map[string]any{}},
			map[string]any{"api_name": "Stage", "display_label": "Stage", "data_type": "picklist", "system_mandatory": true, "pick_list_values": stages, "unique": map[string]any{},
				"profiles": []any{map[string]any{"name": "SENTINEL profile"}}})
	}
	server.writeValue(response, http.StatusOK, map[string]any{"fields": fields})
}

// findDuplicate checks the duplicate-check fields in order; text compares without regard to case. The caller holds the mutex.
func (server *Server) findDuplicate(module string, fields map[string]any, duplicateCheckFields []string) (map[string]any, any) {
	for _, field := range duplicateCheckFields {
		wanted, isText := fields[field].(string)
		if !isText || wanted == "" {
			continue
		}
		for _, record := range server.sortedRecords(module) {
			if existing, isText := record[field].(string); isText && strings.EqualFold(existing, wanted) {
				return record, field
			}
		}
	}
	return nil, nil
}

// rejectedStage enforces the Deals Stage picklist, as Zoho CRM does. The caller holds the mutex.
func (server *Server) rejectedStage(module string, fields map[string]any) map[string]any {
	stage, isText := fields["Stage"].(string)
	if module != "Deals" || !isText || slices.Contains(DealStages, stage) {
		return nil
	}
	return recordError("INVALID_DATA", "Stage")
}

// applyFields writes fields; a lookup {"id"} gains the name Zoho CRM shows. The caller holds the mutex.
func (server *Server) applyFields(record map[string]any, fields map[string]any) {
	for key, value := range fields {
		if lookup, isLookup := value.(map[string]any); isLookup {
			recordID, _ := lookup["id"].(string)
			value = map[string]any{"id": recordID, "name": server.lookupName(key, recordID)}
		}
		record[key] = value
	}
}

func (server *Server) lookupName(field string, recordID string) string {
	switch field {
	case "Account_Name":
		name, _ := server.records["Accounts"][recordID]["Account_Name"].(string)
		return name
	case "Contact_Name":
		name, _ := server.records["Contacts"][recordID]["Last_Name"].(string)
		return name
	default:
		return "Fake Owner"
	}
}

// storeRecord adds a record; the caller holds the mutex.
func (server *Server) storeRecord(module string, fields map[string]any, modifiedAt time.Time) map[string]any {
	server.nextID++
	record := map[string]any{"id": strconv.FormatInt(server.nextID, 10), "Created_Time": formatZohoTime(modifiedAt), "Modified_Time": formatZohoTime(modifiedAt),
		"Owner": map[string]any{"id": "4150868000000225013", "name": "Fake Owner"}}
	server.applyFields(record, fields)
	if server.records[module] == nil {
		server.records[module] = map[string]map[string]any{}
	}
	server.records[module][record["id"].(string)] = record
	return record
}

// sortedRecords lists a module's records oldest ID first; the caller holds the mutex.
func (server *Server) sortedRecords(module string) []map[string]any {
	var records []map[string]any
	for _, record := range server.records[module] {
		records = append(records, record)
	}
	sortRecords(records, []orderTerm{{field: "id"}})
	return records
}

func (server *Server) advanceClock() time.Time {
	server.clock = server.clock.Add(time.Second)
	return server.clock
}

func (server *Server) sleepBeforeAnswering() {
	server.delayedRequests.Add(1)
	defer server.delayedRequests.Done()
	time.Sleep(SlowResponseDelay)
}

func (server *Server) record(name string, request *http.Request, body []byte, selectQuery string) int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.counts[name]++
	recorded := Request{At: time.Now(), Query: request.URL.Query(), Body: string(body), SelectQuery: selectQuery}
	if form, err := url.ParseQuery(string(body)); err == nil && name == "token" {
		recorded.Form = form
	}
	server.requests[name] = append(server.requests[name], recorded)
	return server.counts[name]
}

func (server *Server) writeValue(response http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		server.t.Errorf("fake Zoho CRM response: %v", err)
	}
	server.writeJSON(response, status, string(encoded))
}

func (server *Server) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json;charset=UTF-8")
	response.WriteHeader(status)
	if _, err := io.WriteString(response, body); err != nil {
		server.t.Logf("fake Zoho CRM response write failed: %v", err)
	}
}

func writeSuccess(action string, record map[string]any, duplicateField any) map[string]any {
	entry := map[string]any{"code": "SUCCESS", "status": "success", "message": "SENTINEL record written", "details": map[string]any{
		"id": record["id"], "Created_Time": record["Created_Time"], "Modified_Time": record["Modified_Time"],
		"Modified_By": map[string]any{"id": "4150868000000225013", "name": "Fake Owner"},
	}}
	if action != "" {
		entry["action"], entry["duplicate_field"] = action, duplicateField
	}
	return map[string]any{"data": []any{entry}}
}

func recordError(code string, apiName string) map[string]any {
	return map[string]any{"data": []any{map[string]any{
		"code": code, "status": "error", "message": "SENTINEL " + code, "details": map[string]any{"api_name": apiName, "json_path": "$.data[0]." + apiName},
	}}}
}

func formatZohoTime(instant time.Time) string {
	return instant.In(zohoZone).Format("2006-01-02T15:04:05-07:00")
}

// dropConnection closes the connection after the request arrived, as a lost response would.
func dropConnection(response http.ResponseWriter) {
	hijacker, isHijacker := response.(http.Hijacker)
	if !isHijacker {
		return
	}
	connection, _, err := hijacker.Hijack()
	if err == nil {
		// Closing is the point: the client sees a lost response either way.
		_ = connection.Close()
	}
}
