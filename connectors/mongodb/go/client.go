// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package mongodb exposes typed, bounded MongoDB document Queries to Dex Flows.
package mongodb

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"go.mongodb.org/mongo-driver/mongo"
)

var namespacePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
var collectionPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

// Option reserves constructor-only transport configuration for future releases.
type Option interface{ applyClientOption(*Client) }

// Client stores non-secret query bounds and resolves the connection URI per call.
type Client struct {
	database         string
	timeout          time.Duration
	maxDocuments     int64
	maxResponseBytes int64
	credentials      sdkgo.CredentialProvider[Credentials]
	mongoClient      *mongo.Client
}

// New validates settings and credentials without contacting MongoDB.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	client, err := newClient(config, credentials, nil)
	if err != nil {
		return nil, err
	}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("MongoDB client option is required")
		}
		option.applyClientOption(client)
	}
	return client, nil
}

func newClient(config Config, credentials sdkgo.CredentialProvider[Credentials], mongoClient *mongo.Client) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("MongoDB credential provider is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !namespacePattern.MatchString(config.Database) {
		return nil, fmt.Errorf("MongoDB database name is invalid")
	}
	if config.MaxDocuments > 1000 || config.MaxResponseBytes > 16<<20 {
		return nil, fmt.Errorf("MongoDB result bounds exceed the connector maximum")
	}
	return &Client{
		database: config.Database, timeout: config.Timeout, maxDocuments: config.MaxDocuments,
		maxResponseBytes: config.MaxResponseBytes, credentials: credentials, mongoClient: mongoClient,
	}, nil
}

// FindDocuments binds the read-only Query to this client.
func (client *Client) FindDocuments() FindDocumentsOperation {
	return FindDocumentsOperation{client: client}
}
