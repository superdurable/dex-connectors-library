// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// LoadedProject contains an immutable ordinary configuration snapshot and independently resolved current credentials.
type LoadedProject struct {
	objects        ObjectStore
	scope          Scope
	configurations *ConfigurationStore
	// Connections resolves private credential material through the fixed project boundary.
	Connections *ConnectionStore
	// Configuration is the exact ordinary snapshot accepted for this application instance.
	Configuration Configuration
	// Snapshot identifies the exact immutable configuration object version and digest.
	Snapshot SnapshotRef
}

// LoadFromEnvironment loads the trusted DEX_PROJECT_* deployment contract and AWS default credential chain.
// It reads only the pinned ordinary snapshot at startup; no credential read or provider refresh occurs.
// Hosted storage requires an exact KMS key ARN. Local S3 endpoints require explicit local-storage opt-in.
func LoadFromEnvironment(ctx context.Context) (*LoadedProject, error) {
	scope := Scope{ProjectID: os.Getenv("DEX_PROJECT_ID"), Kind: os.Getenv("DEX_PROJECT_SCOPE"), SessionID: os.Getenv("DEX_PROJECT_SESSION_ID")}
	if _, err := scope.Prefix(); err != nil {
		return nil, err
	}
	allowLocal := os.Getenv("DEX_PROJECT_ALLOW_LOCAL_STORAGE")
	if allowLocal != "" && allowLocal != "false" && allowLocal != "true" {
		return nil, errors.New("project local storage opt-in must be true or false")
	}
	endpoint := os.Getenv("DEX_PROJECT_STORAGE_ENDPOINT")
	if endpoint != "" && (allowLocal != "true" || !isLocalStorageEndpoint(endpoint)) {
		return nil, errors.New("custom project storage endpoint requires an explicit local fixture boundary")
	}
	if allowLocal == "true" && endpoint == "" {
		return nil, errors.New("local project storage requires an explicit local endpoint")
	}
	bucket, prefix, kmsKey := os.Getenv("DEX_PROJECT_STORAGE_BUCKET"), os.Getenv("DEX_PROJECT_STORAGE_PREFIX"), os.Getenv("DEX_PROJECT_STORAGE_KMS_KEY_ARN")
	if bucket == "" || (allowLocal != "true" && !kmsKeyARN.MatchString(kmsKey)) {
		return nil, errors.New("project storage bucket and exact hosted KMS key ARN are required")
	}
	awsConfig, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, errors.New("project AWS credential configuration is unavailable")
	}
	client := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		if endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
			options.UsePathStyle = true
		}
	})
	objects, err := NewS3ObjectStore(ctx, &S3StoreConfig{Client: client, Bucket: bucket, Prefix: prefix, KMSKeyID: kmsKey, AllowUnencrypted: allowLocal == "true"})
	if err != nil {
		return nil, err
	}
	connections, err := NewConnectionStore(&ConnectionStoreConfig{Objects: objects, Scope: scope})
	if err != nil {
		return nil, err
	}
	configurations, err := NewConfigurationStore(&ConfigurationStoreConfig{Objects: objects, Scope: scope})
	if err != nil {
		return nil, err
	}
	reference := SnapshotRef{Key: os.Getenv("DEX_PROJECT_CONFIG_KEY"), Version: os.Getenv("DEX_PROJECT_CONFIG_VERSION"), Digest: os.Getenv("DEX_PROJECT_CONFIG_DIGEST"), MediaType: "application/json"}
	configuration, err := configurations.ReadSnapshot(ctx, reference)
	if err != nil {
		return nil, err
	}
	return &LoadedProject{objects: objects, scope: scope, configurations: configurations, Connections: connections, Configuration: configuration, Snapshot: reference}, nil
}

// TriggerInbox binds one Trigger binding's durable inbox in the loaded project scope.
func (project *LoadedProject) TriggerInbox(key TriggerInboxKey) (*TriggerInbox, error) {
	return NewTriggerInbox(project.objects, project.scope, key)
}

// ResolveApplicationEnvironment loads exact pinned app secrets without provider refresh or mutable configuration reads.
// Resolve and optionally Apply the result before starting application goroutines; never place returned values in a Flow.
func (project *LoadedProject) ResolveApplicationEnvironment(ctx context.Context) (ApplicationEnvironment, error) {
	return project.configurations.ResolveApplicationEnvironment(ctx, project.Configuration)
}

func isLocalStorageEndpoint(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	hostname := parsed.Hostname()
	address := net.ParseIP(hostname)
	return hostname == "localhost" || hostname == "host.docker.internal" || strings.HasSuffix(hostname, ".svc.cluster.local") || (address != nil && (address.IsLoopback() || address.IsPrivate()))
}
