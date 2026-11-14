// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bucketlist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/qiniu/go"
	"github.com/superdurable/dex-connectors-library/connectors/qiniu/go/qiniumock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestBucketListFlowIdentityAndMapping(t *testing.T) {
	input := qiniu.ListBucketsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(qiniu.Connection{})))
	require.Equal(t, "StartListBuckets", dex.GetFinalStepType[qiniu.ListBucketsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := qiniumock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
