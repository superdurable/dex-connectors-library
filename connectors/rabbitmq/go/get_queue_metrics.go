// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package rabbitmq

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetQueueMetricsInput identifies one queue within one virtual host.
type GetQueueMetricsInput struct {
	// VirtualHost is the exact RabbitMQ virtual host, commonly /.
	VirtualHost string `json:"virtualHost"`
	// Name is the exact queue name displayed in the Management UI.
	Name string `json:"name"`
}

// QueueMetrics contains bounded queue identity and current counts.
type QueueMetrics struct {
	// VirtualHost is the virtual host returned by RabbitMQ.
	VirtualHost string `json:"virtualHost"`
	// Name is the queue name returned by RabbitMQ.
	Name string `json:"name"`
	// Messages is the total current message count.
	Messages int64 `json:"messages"`
	// Consumers is the current consumer count.
	Consumers int64 `json:"consumers"`
	// State is the provider's current queue state.
	State string `json:"state"`
}

// GetQueueMetricsOperation reads the Management API's single-queue endpoint.
type GetQueueMetricsOperation struct{ client *Client }

// GetQueueMetrics binds a queue-metrics Query to this client.
func (client *Client) GetQueueMetrics() GetQueueMetricsOperation {
	return GetQueueMetricsOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (GetQueueMetricsOperation) Definition() sdkgo.QueryDefinition { return GetQueueMetricsDefinition }

// Invoke reads one queue and classifies transport, rate-limit, and provider outcomes.
func (operation GetQueueMetricsOperation) Invoke(call sdkgo.Call, input GetQueueMetricsInput) sdkgo.QueryAttempt[QueueMetrics] {
	if !validQueueIdentity(input.VirtualHost) || !validQueueIdentity(input.Name) {
		return queueBranch(GetQueueMetricsBranchDefect, sdkgo.FailureValidation, "virtual host or queue name is invalid")
	}
	response := operation.client.getQueue(call, input.VirtualHost, input.Name)
	if errors.Is(response.err, errCredential) {
		return queueBranch(GetQueueMetricsBranchDefect, sdkgo.FailureAuthentication, "RabbitMQ credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[QueueMetrics](queueFailure(sdkgo.FailureTransport, "RabbitMQ request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[QueueMetrics](queueFailure(sdkgo.FailureRateLimit, "RabbitMQ rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[QueueMetrics](queueFailure(sdkgo.FailureAvailability, "RabbitMQ is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return queueBranch(GetQueueMetricsBranchNotFound, sdkgo.FailureProviderRejection, "RabbitMQ queue or virtual host was not found")
	}
	if response.status != http.StatusOK {
		return queueBranch(GetQueueMetricsBranchProviderRejected, sdkgo.FailureProviderRejection, rejectedStatus(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queueBranch(GetQueueMetricsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "RabbitMQ response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[QueueMetrics](queueFailure(sdkgo.FailureTransport, "RabbitMQ response was interrupted"), 0)
	}
	metrics, err := decodeQueueMetrics(response.body, input)
	if err != nil {
		return queueBranch(GetQueueMetricsBranchInvalidResponse, sdkgo.FailureProtocol, "RabbitMQ returned invalid queue metrics")
	}
	return sdkgo.NewQueryBranch(GetQueueMetricsBranchFound, metrics, nil, sdkgo.Receipt{})
}

func decodeQueueMetrics(body []byte, input GetQueueMetricsInput) (QueueMetrics, error) {
	var wire struct {
		VirtualHost string `json:"vhost"`
		Name        string `json:"name"`
		Messages    *int64 `json:"messages"`
		Consumers   *int64 `json:"consumers"`
		State       string `json:"state"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return QueueMetrics{}, err
	}
	if wire.VirtualHost != input.VirtualHost || wire.Name != input.Name || wire.Messages == nil || wire.Consumers == nil || *wire.Messages < 0 || *wire.Consumers < 0 || wire.State == "" {
		return QueueMetrics{}, errors.New("queue metrics are missing or inconsistent")
	}
	return QueueMetrics{VirtualHost: wire.VirtualHost, Name: wire.Name, Messages: *wire.Messages, Consumers: *wire.Consumers, State: wire.State}, nil
}
