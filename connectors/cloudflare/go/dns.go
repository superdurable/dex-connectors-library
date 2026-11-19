// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudflare

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// PurgeEntireCacheInput identifies one zone whose complete cache should be purged.
type PurgeEntireCacheInput struct {
	// ZoneID is the Cloudflare zone identifier.
	ZoneID string `json:"zoneId"`
}

// ListDNSRecordsInput selects one bounded DNS record page.
type ListDNSRecordsInput struct {
	// ZoneID is the Cloudflare zone identifier.
	ZoneID string `json:"zoneId"`
	// Page is the one-based page number; zero selects the first page.
	Page int `json:"page,omitempty"`
	// PerPage limits records per page; zero uses 100.
	PerPage int `json:"perPage,omitempty"`
}

// DNSRecord is the public part of one Cloudflare DNS record.
type DNSRecord struct {
	// ID identifies the record within its zone.
	ID string `json:"id"`
	// Type is the DNS record type.
	Type string `json:"type"`
	// Name is the full DNS name.
	Name string `json:"name"`
	// Content is the record value.
	Content string `json:"content"`
	// TTL is the provider TTL in seconds; one means automatic.
	TTL int `json:"ttl"`
	// Proxied indicates whether Cloudflare proxies the record.
	Proxied bool `json:"proxied"`
}

// DNSRecordsPage contains a bounded record page and provider pagination.
type DNSRecordsPage struct {
	// Records contains the DNS records on this page.
	Records []DNSRecord `json:"records"`
	// Page is the one-based current page.
	Page int `json:"page"`
	// TotalPages is the provider page count.
	TotalPages int `json:"totalPages"`
}

// GetDNSRecordInput identifies one DNS record.
type GetDNSRecordInput struct {
	// ZoneID identifies the zone.
	ZoneID string `json:"zoneId"`
	// RecordID identifies the DNS record.
	RecordID string `json:"recordId"`
}

// UpdateDNSRecordInput overwrites one record with an absolute value.
type UpdateDNSRecordInput struct {
	// ZoneID identifies the zone.
	ZoneID string `json:"zoneId"`
	// RecordID identifies the DNS record.
	RecordID string `json:"recordId"`
	// Type is A, AAAA, CNAME, or TXT.
	Type string `json:"type"`
	// Name is the full DNS name.
	Name string `json:"name"`
	// Content is the address, target, or text value.
	Content string `json:"content"`
	// TTL is seconds, or one for Cloudflare automatic TTL.
	TTL int `json:"ttl"`
	// Proxied enables Cloudflare proxying for eligible records.
	Proxied bool `json:"proxied"`
}

// DeleteDNSRecordInput identifies one record to remove.
type DeleteDNSRecordInput struct {
	// ZoneID identifies the zone.
	ZoneID string `json:"zoneId"`
	// RecordID identifies the DNS record.
	RecordID string `json:"recordId"`
}

// DeletedDNSRecord identifies the record Cloudflare deleted.
type DeletedDNSRecord struct {
	// ID is the deleted record identifier.
	ID string `json:"id"`
}

// PurgeEntireCacheOperation purges all cached assets in one zone.
type PurgeEntireCacheOperation struct{ client *Client }

// PurgeEntireCache binds the Mutation to this client.
func (client *Client) PurgeEntireCache() PurgeEntireCacheOperation {
	return PurgeEntireCacheOperation{client}
}

// Definition returns the generated purgeEntireCache contract.
func (PurgeEntireCacheOperation) Definition() sdkgo.MutationDefinition {
	return PurgeEntireCacheDefinition
}

// IdempotencyKey correlates the repeat-safe cache removal attempts.
func (PurgeEntireCacheOperation) IdempotencyKey(callID sdkgo.CallID, _ PurgeEntireCacheInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke validates the zone and classifies the purge response.
func (operation PurgeEntireCacheOperation) Invoke(call sdkgo.Call, input PurgeEntireCacheInput) sdkgo.MutationAttempt[CachePurge] {
	if !validCloudflareID(input.ZoneID) {
		return sdkgo.NewMutationBranch(PurgeEntireCacheBranchDefect, CachePurge{}, failure(sdkgo.FailureValidation, "purgeEntireCache", "zoneId is invalid"), sdkgo.Receipt{})
	}
	spec := requestSpec{method: http.MethodPost, path: "/zones/" + url.PathEscape(input.ZoneID) + "/purge_cache", body: []byte(`{"purge_everything":true}`), contentType: "application/json"}
	return executeMutation(operation.client, call, "purgeEntireCache", spec, PurgeEntireCacheBranchPurged, PurgeEntireCacheBranchNotFound, PurgeEntireCacheBranchProviderRejected, PurgeEntireCacheBranchInvalidResponse, PurgeEntireCacheBranchDefect, decodePurgeCacheByURLs)
}

// ListDNSRecordsOperation reads a bounded page of DNS records.
type ListDNSRecordsOperation struct{ client *Client }

// ListDNSRecords binds the Query to this client.
func (client *Client) ListDNSRecords() ListDNSRecordsOperation {
	return ListDNSRecordsOperation{client}
}

// Definition returns the generated listDNSRecords contract.
func (ListDNSRecordsOperation) Definition() sdkgo.QueryDefinition { return ListDNSRecordsDefinition }

// Invoke validates pagination and classifies the DNS record page.
func (operation ListDNSRecordsOperation) Invoke(call sdkgo.Call, input ListDNSRecordsInput) sdkgo.QueryAttempt[DNSRecordsPage] {
	if !validCloudflareID(input.ZoneID) || input.Page < 0 || input.PerPage < 0 || input.PerPage > 100 {
		return sdkgo.NewQueryBranch(ListDNSRecordsBranchDefect, DNSRecordsPage{}, failure(sdkgo.FailureValidation, "listDNSRecords", "zoneId or pagination is invalid"), sdkgo.Receipt{})
	}
	page, perPage := input.Page, input.PerPage
	if page == 0 {
		page = 1
	}
	if perPage == 0 {
		perPage = 100
	}
	spec := requestSpec{method: http.MethodGet, path: dnsRecordsPath(input.ZoneID), query: url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)}}}
	return executeQuery(operation.client, call, "listDNSRecords", spec, ListDNSRecordsBranchListed, ListDNSRecordsBranchNotFound, ListDNSRecordsBranchProviderRejected, ListDNSRecordsBranchInvalidResponse, ListDNSRecordsBranchDefect, decodeDNSRecordsPage)
}

// GetDNSRecordOperation fetches one DNS record.
type GetDNSRecordOperation struct{ client *Client }

// GetDNSRecord binds the Query to this client.
func (client *Client) GetDNSRecord() GetDNSRecordOperation { return GetDNSRecordOperation{client} }

// Definition returns the generated getDNSRecord contract.
func (GetDNSRecordOperation) Definition() sdkgo.QueryDefinition { return GetDNSRecordDefinition }

// Invoke validates IDs and classifies the DNS record response.
func (operation GetDNSRecordOperation) Invoke(call sdkgo.Call, input GetDNSRecordInput) sdkgo.QueryAttempt[DNSRecord] {
	if !validCloudflareID(input.ZoneID) || !validCloudflareID(input.RecordID) {
		return sdkgo.NewQueryBranch(GetDNSRecordBranchDefect, DNSRecord{}, failure(sdkgo.FailureValidation, "getDNSRecord", "zoneId or recordId is invalid"), sdkgo.Receipt{})
	}
	spec := requestSpec{method: http.MethodGet, path: dnsRecordPath(input.ZoneID, input.RecordID)}
	return executeQuery(operation.client, call, "getDNSRecord", spec, GetDNSRecordBranchFound, GetDNSRecordBranchNotFound, GetDNSRecordBranchProviderRejected, GetDNSRecordBranchInvalidResponse, GetDNSRecordBranchDefect, decodeDNSRecord)
}

// UpdateDNSRecordOperation overwrites one DNS record.
type UpdateDNSRecordOperation struct{ client *Client }

// UpdateDNSRecord binds the Mutation to this client.
func (client *Client) UpdateDNSRecord() UpdateDNSRecordOperation {
	return UpdateDNSRecordOperation{client}
}

// Definition returns the generated updateDNSRecord contract.
func (UpdateDNSRecordOperation) Definition() sdkgo.MutationDefinition {
	return UpdateDNSRecordDefinition
}

// IdempotencyKey correlates attempts; overwriting with the same value is repeat safe.
func (UpdateDNSRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateDNSRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke validates the record replacement and classifies the response.
func (operation UpdateDNSRecordOperation) Invoke(call sdkgo.Call, input UpdateDNSRecordInput) sdkgo.MutationAttempt[DNSRecord] {
	if err := validateDNSReplacement(input); err != nil {
		return sdkgo.NewMutationBranch(UpdateDNSRecordBranchDefect, DNSRecord{}, failure(sdkgo.FailureValidation, "updateDNSRecord", err.Error()), sdkgo.Receipt{})
	}
	body, err := json.Marshal(struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		Content string `json:"content"`
		TTL     int    `json:"ttl"`
		Proxied bool   `json:"proxied"`
	}{input.Type, input.Name, input.Content, input.TTL, input.Proxied})
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateDNSRecordBranchDefect, DNSRecord{}, failure(sdkgo.FailureLocalDefect, "updateDNSRecord", "record could not be encoded"), sdkgo.Receipt{})
	}
	spec := requestSpec{method: http.MethodPut, path: dnsRecordPath(input.ZoneID, input.RecordID), body: body, contentType: "application/json"}
	return executeMutation(operation.client, call, "updateDNSRecord", spec, UpdateDNSRecordBranchUpdated, UpdateDNSRecordBranchNotFound, UpdateDNSRecordBranchProviderRejected, UpdateDNSRecordBranchInvalidResponse, UpdateDNSRecordBranchDefect, decodeDNSRecord)
}

// DeleteDNSRecordOperation removes one DNS record.
type DeleteDNSRecordOperation struct{ client *Client }

// DeleteDNSRecord binds the Mutation to this client.
func (client *Client) DeleteDNSRecord() DeleteDNSRecordOperation {
	return DeleteDNSRecordOperation{client}
}

// Definition returns the generated deleteDNSRecord contract.
func (DeleteDNSRecordOperation) Definition() sdkgo.MutationDefinition {
	return DeleteDNSRecordDefinition
}

// IdempotencyKey correlates attempts; deleting the same record is repeat safe.
func (DeleteDNSRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteDNSRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke validates IDs and classifies the deletion response.
func (operation DeleteDNSRecordOperation) Invoke(call sdkgo.Call, input DeleteDNSRecordInput) sdkgo.MutationAttempt[DeletedDNSRecord] {
	if !validCloudflareID(input.ZoneID) || !validCloudflareID(input.RecordID) {
		return sdkgo.NewMutationBranch(DeleteDNSRecordBranchDefect, DeletedDNSRecord{}, failure(sdkgo.FailureValidation, "deleteDNSRecord", "zoneId or recordId is invalid"), sdkgo.Receipt{})
	}
	spec := requestSpec{method: http.MethodDelete, path: dnsRecordPath(input.ZoneID, input.RecordID)}
	return executeMutation(operation.client, call, "deleteDNSRecord", spec, DeleteDNSRecordBranchDeleted, DeleteDNSRecordBranchNotFound, DeleteDNSRecordBranchProviderRejected, DeleteDNSRecordBranchInvalidResponse, DeleteDNSRecordBranchDefect, decodeDeletedDNSRecord)
}

func validCloudflareID(identifier string) bool {
	if len(identifier) == 0 || len(identifier) > 32 {
		return false
	}
	for _, character := range identifier {
		if character != '-' && character != '_' && (character < '0' || character > '9') && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
			return false
		}
	}
	return true
}

func dnsRecordsPath(zoneID string) string { return "/zones/" + url.PathEscape(zoneID) + "/dns_records" }
func dnsRecordPath(zoneID, recordID string) string {
	return dnsRecordsPath(zoneID) + "/" + url.PathEscape(recordID)
}

func validateDNSReplacement(input UpdateDNSRecordInput) error {
	if !validCloudflareID(input.ZoneID) || !validCloudflareID(input.RecordID) {
		return errors.New("zoneId and recordId must be Cloudflare identifiers")
	}
	return validateDNSRecordFields(input.Type, input.Name, input.Content, input.TTL, input.Proxied)
}

func validateDNSRecordFields(recordType, name, content string, ttl int, isProxied bool) error {
	if name == "" || len(name) > 255 || !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) || strings.TrimSpace(name) != name {
		return errors.New("name must be a complete DNS name")
	}
	if content == "" || len(content) > 2048 || !utf8.ValidString(content) || strings.ContainsFunc(content, unicode.IsControl) {
		return errors.New("content is invalid")
	}
	if ttl != 1 && (ttl < 60 || ttl > 86400) {
		return errors.New("ttl must be automatic (1) or 60 to 86400 seconds")
	}
	switch recordType {
	case "A":
		if address := net.ParseIP(content); address == nil || address.To4() == nil {
			return errors.New("A content must be an IPv4 address")
		}
	case "AAAA":
		if address := net.ParseIP(content); address == nil || address.To4() != nil {
			return errors.New("AAAA content must be an IPv6 address")
		}
	case "CNAME":
		if strings.ContainsAny(content, " /:@") {
			return errors.New("CNAME content must be a DNS name")
		}
	case "TXT":
		if isProxied {
			return errors.New("TXT records cannot be proxied")
		}
	default:
		return errors.New("type must be A, AAAA, CNAME, or TXT")
	}
	return nil
}

func decodeDNSRecord(body []byte) (DNSRecord, error) {
	var wire struct {
		Success bool      `json:"success"`
		Result  DNSRecord `json:"result"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return DNSRecord{}, err
	}
	if !wire.Success || !validCloudflareID(wire.Result.ID) || wire.Result.Type == "" || wire.Result.Name == "" || wire.Result.Content == "" {
		return DNSRecord{}, fmt.Errorf("incomplete DNS record")
	}
	return wire.Result, nil
}

func decodeDNSRecordsPage(body []byte) (DNSRecordsPage, error) {
	var wire struct {
		Success    bool        `json:"success"`
		Result     []DNSRecord `json:"result"`
		ResultInfo struct {
			Page       int `json:"page"`
			TotalPages int `json:"total_pages"`
		} `json:"result_info"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return DNSRecordsPage{}, err
	}
	if !wire.Success || wire.Result == nil || wire.ResultInfo.Page < 1 || wire.ResultInfo.TotalPages < 1 {
		return DNSRecordsPage{}, fmt.Errorf("incomplete DNS record page")
	}
	for _, record := range wire.Result {
		if !validCloudflareID(record.ID) || record.Type == "" || record.Name == "" || record.Content == "" {
			return DNSRecordsPage{}, fmt.Errorf("incomplete DNS record")
		}
	}
	return DNSRecordsPage{Records: wire.Result, Page: wire.ResultInfo.Page, TotalPages: wire.ResultInfo.TotalPages}, nil
}

func decodeDeletedDNSRecord(body []byte) (DeletedDNSRecord, error) {
	var wire struct {
		Success bool             `json:"success"`
		Result  DeletedDNSRecord `json:"result"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return DeletedDNSRecord{}, err
	}
	if !wire.Success || !validCloudflareID(wire.Result.ID) {
		return DeletedDNSRecord{}, fmt.Errorf("incomplete deleted DNS record")
	}
	return wire.Result, nil
}
