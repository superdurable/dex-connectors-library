// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wecom_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/tencent/wecom/go"
	"gopkg.in/yaml.v3"
)

// guidanceManifest is the part of connector.yaml that Dex Web renders on the Connections page.
type guidanceManifest struct {
	Spec struct {
		Configuration struct {
			Fields []guidanceField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			DefaultMethod string `yaml:"defaultMethod"`
			Methods       []struct {
				ID     string          `yaml:"id"`
				Type   string          `yaml:"type"`
				Fields []guidanceField `yaml:"fields"`
				Guide  struct {
					StartURL string   `yaml:"startURL"`
					Steps    []string `yaml:"steps"`
				} `yaml:"guide"`
			} `yaml:"methods"`
		} `yaml:"auth"`
		Operations []struct {
			Name      string `yaml:"name"`
			Execution struct {
				Durability    string `yaml:"durability"`
				DispatchFence bool   `yaml:"dispatchFence"`
				Retry         any    `yaml:"retry"`
			} `yaml:"execution"`
			Branches []struct {
				ID string `yaml:"id"`
			} `yaml:"branches"`
		} `yaml:"operations"`
		Studio any `yaml:"studio"`
	} `yaml:"spec"`
}

type guidanceField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default"`
	Description string `yaml:"description"`
}

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	visibleFields := append([]guidanceField(nil), manifest.Spec.Configuration.Fields...)
	for _, method := range manifest.Spec.Auth.Methods {
		visibleFields = append(visibleFields, method.Fields...)
	}
	var names []string
	for _, field := range visibleFields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"baseUrl", "timeout", "maxResponseBytes", "webhook_key", "corp_id", "agent_id", "corp_secret"}, names, "a new visible field needs its own guidance review")
	for _, field := range visibleFields {
		t.Run(field.Name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(field.Description), 120, "a label-length description is not guidance")
			if field.Default != nil {
				require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web already shows the default in parentheses")
			}
			require.Contains(t, strings.ToLower(field.Description), "blank", "every field explains what blank means")
			if field.Type == "secretString" {
				require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
				require.Contains(t, field.Description, "Dex credential")
			}
		})
	}
	fieldsByName := map[string]guidanceField{}
	for _, field := range visibleFields {
		fieldsByName[field.Name] = field
	}
	for name, texts := range map[string][]string{
		"webhook_key": {"消息推送", "群机器人", "key=", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send"},
		"corp_id":     {"https://work.weixin.qq.com/wework_admin/frame#profile", "企业ID", "ww"},
		"agent_id":    {"https://work.weixin.qq.com/wework_admin/frame#apps", "AgentId", "positive decimal integer"},
		"corp_secret": {"https://work.weixin.qq.com/wework_admin/frame#apps", "Secret", "查看"},
	} {
		require.True(t, fieldsByName[name].Required, name)
		for _, text := range texts {
			require.Contains(t, fieldsByName[name].Description, text, name)
		}
	}
	require.Nil(t, manifest.Spec.Studio, "the connector ships no Studio bundle; recipients come from Flow input")
}

func TestTheAuthorizationGuidesNameEveryConsoleStep(t *testing.T) {
	auth := readGuidanceManifest(t).Spec.Auth
	require.Equal(t, "group-robot", auth.DefaultMethod)
	require.Len(t, auth.Methods, 2)
	expectations := map[string]struct {
		startURL string
		texts    []string
	}{
		"group-robot": {
			startURL: "https://developer.work.weixin.qq.com/document/path/91770",
			texts:    []string{"internal WeCom group chat", "消息推送", "群机器人", "添加", "key=", "secret", "20 messages per minute", "remove the robot"},
		},
		"application": {
			startURL: "https://work.weixin.qq.com/wework_admin/frame#apps",
			texts:    []string{"应用管理", "创建应用", "可见范围", "81013", "AgentId", "查看", "企业ID", "企业可信IP", "egress IP", "60020", "可信域名", "reset"},
		},
	}
	for _, method := range auth.Methods {
		expected, ok := expectations[method.ID]
		require.True(t, ok, method.ID)
		require.Equal(t, "apiKey", method.Type, "webhook keys and application secrets are static credentials")
		require.Equal(t, expected.startURL, method.Guide.StartURL)
		guide := strings.Join(method.Guide.Steps, " ")
		for _, text := range expected.texts {
			require.Contains(t, guide, text, method.ID)
		}
	}
}

func TestOperationExecutionMatchesTheirRepeatSafety(t *testing.T) {
	operations := readGuidanceManifest(t).Spec.Operations
	require.Len(t, operations, 2)
	robot, application := operations[0], operations[1]
	require.Equal(t, "sendGroupRobotMessage", robot.Name)
	require.Equal(t, "sync", robot.Execution.Durability, "the single-dispatch fence needs sync heartbeats")
	require.True(t, robot.Execution.DispatchFence)
	require.Equal(t, []string{"sent", "providerRejected", "uncertain", "defect"}, branchIDs(robot.Branches))
	require.Equal(t, "sendApplicationMessage", application.Name)
	require.Equal(t, "async", application.Execution.Durability)
	require.False(t, application.Execution.DispatchFence)
	require.Nil(t, application.Execution.Retry, "the default one-hour retry window must stay inside the four-hour duplicate check")
	require.Equal(t, []string{"sent", "providerRejected", "uncertain", "defect"}, branchIDs(application.Branches), "uncertain covers only a retry past the duplicate check window")
}

func TestREADMENamesTheManifestDefaults(t *testing.T) {
	readme, err := os.ReadFile("../README.md")
	require.NoError(t, err)
	defaults := wecom.DefaultConfig()
	require.Contains(t, string(readme), "| `baseUrl` | `"+defaults.BaseURL+"` |")
	require.Contains(t, string(readme), "| `timeout` | `"+defaults.Timeout.String()+"` |")
	require.Contains(t, string(readme), "| `maxResponseBytes` | `"+strconv.FormatInt(defaults.MaxResponseBytes, 10)+"` |")
	require.Contains(t, string(readme), strconv.FormatInt(int64(wecom.DuplicateCheckInterval/time.Second), 10)+" seconds")
}

func branchIDs(branches []struct {
	ID string `yaml:"id"`
}) []string {
	var identifiers []string
	for _, branch := range branches {
		identifiers = append(identifiers, branch.ID)
	}
	return identifiers
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("../connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}
