// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qingcloud

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// DescribeZonesInput has no parameters; QingCloud lists accessible zones.
type DescribeZonesInput struct{}

// Zone describes one accessible IaaS region.
type Zone struct {
	// ZoneID is the QingCloud zone identifier.
	ZoneID string `json:"zoneId"`
	// Status is active, faulty, or defunct.
	Status string `json:"status"`
}

// ZonesPage contains at most 100 accessible zones and the provider's total count.
type ZonesPage struct {
	// Zones is the page returned by QingCloud.
	Zones []Zone `json:"zones"`
	// TotalCount is the number of zones matching the query.
	TotalCount int `json:"totalCount"`
}

// DescribeZonesOperation lists accessible IaaS zones.
type DescribeZonesOperation struct{ client *Client }

// DescribeZones binds the Query to this client.
func (client *Client) DescribeZones() DescribeZonesOperation { return DescribeZonesOperation{client} }

// Definition returns the manifest-generated Query worker.
func (DescribeZonesOperation) Definition() sdkgo.QueryDefinition { return DescribeZonesDefinition }

// Invoke queries QingCloud and classifies the provider outcome.
func (operation DescribeZonesOperation) Invoke(call sdkgo.Call, _ DescribeZonesInput) sdkgo.QueryAttempt[ZonesPage] {
	response := operation.client.describeZones(call)
	if errors.Is(response.err, errCredential) {
		return zonesBranch(DescribeZonesBranchDefect, sdkgo.FailureAuthentication, "QingCloud credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[ZonesPage](zonesFailure(sdkgo.FailureTransport, "QingCloud request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[ZonesPage](zonesFailure(sdkgo.FailureRateLimit, "QingCloud rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[ZonesPage](zonesFailure(sdkgo.FailureAvailability, "QingCloud is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return zonesBranch(DescribeZonesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "QingCloud response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[ZonesPage](zonesFailure(sdkgo.FailureTransport, "QingCloud response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return zonesBranch(DescribeZonesBranchProviderRejected, sdkgo.FailureProviderRejection, "QingCloud rejected HTTP "+strconv.Itoa(response.status))
	}
	var wire struct {
		Action     string `json:"action"`
		ReturnCode *int   `json:"ret_code"`
		TotalCount *int   `json:"total_count"`
		ZoneSet    []struct {
			ZoneID string `json:"zone_id"`
			Status string `json:"status"`
		} `json:"zone_set"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return zonesBranch(DescribeZonesBranchInvalidResponse, sdkgo.FailureProtocol, "QingCloud returned invalid zones JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return zonesBranch(DescribeZonesBranchInvalidResponse, sdkgo.FailureProtocol, "QingCloud returned trailing JSON")
	}
	if wire.ReturnCode == nil {
		return zonesBranch(DescribeZonesBranchInvalidResponse, sdkgo.FailureProtocol, "QingCloud omitted ret_code")
	}
	if *wire.ReturnCode != 0 {
		switch *wire.ReturnCode {
		case 5000, 5100, 5200, 5300:
			return sdkgo.NewQueryRetry[ZonesPage](zonesFailure(sdkgo.FailureAvailability, "QingCloud reported a temporary server error"), response.retryAfter)
		default:
			return zonesBranch(DescribeZonesBranchProviderRejected, sdkgo.FailureProviderRejection, "QingCloud rejected the query with code "+strconv.Itoa(*wire.ReturnCode))
		}
	}
	if wire.Action != "DescribeZonesResponse" || wire.TotalCount == nil || *wire.TotalCount < 0 || len(wire.ZoneSet) > 100 || *wire.TotalCount < len(wire.ZoneSet) {
		return zonesBranch(DescribeZonesBranchInvalidResponse, sdkgo.FailureProtocol, "QingCloud returned inconsistent zones metadata")
	}
	zones := make([]Zone, 0, len(wire.ZoneSet))
	for _, item := range wire.ZoneSet {
		if item.ZoneID == "" || (item.Status != "active" && item.Status != "faulty" && item.Status != "defunct") {
			return zonesBranch(DescribeZonesBranchInvalidResponse, sdkgo.FailureProtocol, "QingCloud returned an invalid zone")
		}
		zones = append(zones, Zone{ZoneID: item.ZoneID, Status: item.Status})
	}
	return sdkgo.NewQueryBranch(DescribeZonesBranchListed, ZonesPage{Zones: zones, TotalCount: *wire.TotalCount}, nil, sdkgo.Receipt{})
}
func zonesFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "qingcloud", Operation: "describeZones", Message: message}
}
func zonesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ZonesPage] {
	failure := zonesFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, ZonesPage{}, &failure, sdkgo.Receipt{})
}
