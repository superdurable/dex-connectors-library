// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshsales

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CRMRecordMetadata contains a sales account or deal identity and name.
type CRMRecordMetadata struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// CRMRecordsPage contains one page of sales accounts or deals.
type CRMRecordsPage struct {
	Records []CRMRecordMetadata `json:"records"`
	Page    int                 `json:"page"`
	Total   int                 `json:"total"`
}

// CRMViewMetadata identifies a provider filter view.
type CRMViewMetadata struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// CRMViews lists provider filter views.
type CRMViews struct {
	Views []CRMViewMetadata `json:"views"`
}

// DeletedCRMRecord confirms a sales account or deal ID is absent.
type DeletedCRMRecord struct {
	ID      int64 `json:"id"`
	Deleted bool  `json:"deleted"`
}

type namedRecordBranches struct{ success, notFound, rejected, invalid, defect sdkgo.BranchID }

func invokeNamedViews(call sdkgo.Call, client *Client, operation, path string, branches queryBranches) sdkgo.QueryAttempt[CRMViews] {
	return invokeQuery(call, client, operation, path, branches, decodeCRMViews)
}

func invokeNamedGet(call sdkgo.Call, client *Client, operation, path, envelope string, branches queryBranches) sdkgo.QueryAttempt[CRMRecordMetadata] {
	return invokeQuery(call, client, operation, path, branches, func(body []byte) (CRMRecordMetadata, error) { return decodeCRMRecord(body, envelope) })
}

func invokeNamedList(call sdkgo.Call, client *Client, operation, path, envelope string, page int, branches queryBranches) sdkgo.QueryAttempt[CRMRecordsPage] {
	return invokeQuery(call, client, operation, path, branches, func(body []byte) (CRMRecordsPage, error) { return decodeCRMRecordsPage(body, envelope, page) })
}

func invokeNamedCreate(client *Client, call sdkgo.Call, operation, path, envelope, name string, timeout time.Duration, branches createBranches) sdkgo.MutationAttempt[CRMRecordMetadata] {
	if err := validateCRMName(name); err != nil {
		return createBranch[CRMRecordMetadata](operation, branches.defect, sdkgo.FailureValidation, err.Error())
	}
	requestBody, err := json.Marshal(map[string]any{envelope: map[string]string{"name": name}})
	if err != nil {
		return createBranch[CRMRecordMetadata](operation, branches.defect, sdkgo.FailureLocalDefect, "request cannot be encoded")
	}
	return invokeLimitedCreate(client, call, operation, path, requestBody, timeout, branches, func(body []byte) (CRMRecordMetadata, error) { return decodeCRMRecord(body, envelope) }, func(record CRMRecordMetadata) string { return strconv.FormatInt(record.ID, 10) }, func(id string) string { return path + "/" + id })
}

func invokeNamedUpdate(client *Client, call sdkgo.Call, operation, path, envelope, name string, branches namedRecordBranches) sdkgo.MutationAttempt[CRMRecordMetadata] {
	if err := validateCRMName(name); err != nil {
		return namedBranch(operation, branches.defect, sdkgo.FailureValidation, err.Error())
	}
	body, status, delay, err := client.requestJSON(call, call.Context, http.MethodPut, path, map[string]any{envelope: map[string]string{"name": name}})
	if attempt := classifyNamedUpdate(operation, branches, status, delay, err); attempt != nil {
		return *attempt
	}
	updated, decodeErr := decodeCRMRecord(body, envelope)
	if decodeErr != nil {
		return namedRetry(operation, sdkgo.FailureProtocol, "update answer is unconfirmed", 0)
	}
	confirmedBody, status, delay, err := client.get(call, path)
	if attempt := classifyNamedUpdate(operation, branches, status, delay, err); attempt != nil {
		return *attempt
	}
	confirmed, decodeErr := decodeCRMRecord(confirmedBody, envelope)
	if decodeErr != nil || confirmed.ID != updated.ID || confirmed.Name != name {
		return namedRetry(operation, sdkgo.FailureProtocol, "updated name is not yet confirmed", 0)
	}
	return sdkgo.NewMutationBranch(branches.success, confirmed, nil, createReceipt(call, strconv.FormatInt(confirmed.ID, 10)))
}

func invokeNamedDelete(client *Client, call sdkgo.Call, operation, path string, id int64, success, rejected, defect sdkgo.BranchID) sdkgo.MutationAttempt[DeletedCRMRecord] {
	body, status, delay, err := client.request(call, call.Context, http.MethodDelete, path, nil)
	switch {
	case errors.Is(err, errCredential):
		return deletedNamedBranch(operation, defect, sdkgo.FailureAuthentication, "connection credentials unavailable or invalid")
	case status == http.StatusTooManyRequests:
		return deletedNamedRetry(operation, sdkgo.FailureRateLimit, "Freshsales rate limited deletion", delay)
	case status == http.StatusRequestTimeout || status >= 500 || errors.Is(err, errRequest):
		return deletedNamedRetry(operation, sdkgo.FailureAvailability, "deletion outcome unconfirmed", delay)
	case status == http.StatusNotFound || status == http.StatusNoContent:
		return sdkgo.NewMutationBranch(success, DeletedCRMRecord{ID: id, Deleted: true}, nil, createReceipt(call, strconv.FormatInt(id, 10)))
	case status != http.StatusOK:
		return deletedNamedBranch(operation, rejected, sdkgo.FailureProviderRejection, "Freshsales rejected HTTP "+strconv.Itoa(status))
	case err != nil:
		return deletedNamedRetry(operation, sdkgo.FailureTransport, "deletion response interrupted", 0)
	}
	var deleted bool
	if json.Unmarshal(body, &deleted) != nil || !deleted {
		return deletedNamedRetry(operation, sdkgo.FailureProtocol, "deletion answer unconfirmed", 0)
	}
	return sdkgo.NewMutationBranch(success, DeletedCRMRecord{ID: id, Deleted: true}, nil, createReceipt(call, strconv.FormatInt(id, 10)))
}

func validateCRMName(name string) error {
	if strings.TrimSpace(name) == "" || len(name) > 255 {
		return fmt.Errorf("record name must be 1 to 255 bytes")
	}
	return nil
}

func decodeCRMViews(body []byte) (CRMViews, error) {
	var wire struct {
		Filters []CRMViewMetadata `json:"filters"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CRMViews{}, err
	}
	if wire.Filters == nil {
		return CRMViews{}, fmt.Errorf("view list missing")
	}
	for _, view := range wire.Filters {
		if view.ID <= 0 || view.Name == "" {
			return CRMViews{}, fmt.Errorf("view identity missing")
		}
	}
	return CRMViews{Views: wire.Filters}, nil
}
func decodeCRMRecord(body []byte, envelope string) (CRMRecordMetadata, error) {
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(body, &wire); err != nil {
		return CRMRecordMetadata{}, err
	}
	var record CRMRecordMetadata
	if json.Unmarshal(wire[envelope], &record) != nil || record.ID <= 0 || record.Name == "" {
		return CRMRecordMetadata{}, fmt.Errorf("record identity missing")
	}
	return record, nil
}
func decodeCRMRecordsPage(body []byte, envelope string, pageNumber int) (CRMRecordsPage, error) {
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(body, &wire); err != nil {
		return CRMRecordsPage{}, err
	}
	var records []json.RawMessage
	if json.Unmarshal(wire[envelope], &records) != nil || records == nil {
		return CRMRecordsPage{}, fmt.Errorf("record page missing")
	}
	var meta struct {
		Total int `json:"total"`
	}
	if json.Unmarshal(wire["meta"], &meta) != nil || meta.Total < 0 {
		return CRMRecordsPage{}, fmt.Errorf("record page total missing")
	}
	page := CRMRecordsPage{Records: make([]CRMRecordMetadata, 0, len(records)), Page: pageNumber, Total: meta.Total}
	for _, raw := range records {
		var record CRMRecordMetadata
		if json.Unmarshal(raw, &record) != nil || record.ID <= 0 || record.Name == "" {
			return CRMRecordsPage{}, fmt.Errorf("record identity missing")
		}
		page.Records = append(page.Records, record)
	}
	return page, nil
}

func classifyNamedUpdate(operation string, branches namedRecordBranches, status int, delay time.Duration, err error) *sdkgo.MutationAttempt[CRMRecordMetadata] {
	var attempt sdkgo.MutationAttempt[CRMRecordMetadata]
	switch {
	case errors.Is(err, errCredential):
		attempt = namedBranch(operation, branches.defect, sdkgo.FailureAuthentication, "connection credentials unavailable or invalid")
	case status == http.StatusTooManyRequests:
		attempt = namedRetry(operation, sdkgo.FailureRateLimit, "Freshsales rate limited update", delay)
	case status == http.StatusRequestTimeout || status >= 500 || errors.Is(err, errRequest):
		attempt = namedRetry(operation, sdkgo.FailureAvailability, "update outcome unconfirmed", delay)
	case status == http.StatusNotFound:
		attempt = namedBranch(operation, branches.notFound, sdkgo.FailureNotFound, "Freshsales did not find record")
	case status != http.StatusOK:
		attempt = namedBranch(operation, branches.rejected, sdkgo.FailureProviderRejection, "Freshsales rejected HTTP "+strconv.Itoa(status))
	case err != nil:
		attempt = namedRetry(operation, sdkgo.FailureTransport, "update response interrupted", 0)
	default:
		return nil
	}
	return &attempt
}
func namedBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CRMRecordMetadata] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, CRMRecordMetadata{}, &failure, sdkgo.Receipt{})
}
func namedRetry(operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[CRMRecordMetadata] {
	return sdkgo.NewMutationRetry[CRMRecordMetadata](queryFailure(operation, kind, message), delay)
}
func deletedNamedBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeletedCRMRecord] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, DeletedCRMRecord{}, &failure, sdkgo.Receipt{})
}
func deletedNamedRetry(operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[DeletedCRMRecord] {
	return sdkgo.NewMutationRetry[DeletedCRMRecord](queryFailure(operation, kind, message), delay)
}
