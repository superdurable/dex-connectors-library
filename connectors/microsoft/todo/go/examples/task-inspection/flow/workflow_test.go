// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package flow

import (
	"github.com/stretchr/testify/require"
	listinspection "github.com/superdurable/dex-connectors-library/connectors/microsoft/todo/go/examples/task-inspection/flow/lists"
	taskinspection "github.com/superdurable/dex-connectors-library/connectors/microsoft/todo/go/examples/task-inspection/flow/tasks"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/todo/go/todomock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestBothFlowDefinitionsRegister(t *testing.T) {
	mock := todomock.New(t, listinspection.ConnectionName)
	registry, err := dex.NewRegistry([]dex.Flow{listinspection.NewListFlow(mock.Connection()), taskinspection.NewTaskFlow(mock.Connection())})
	require.NoError(t, err)
	require.NotNil(t, registry)
}
