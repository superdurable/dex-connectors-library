// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package texttranslation

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/youdao/go"
	"github.com/superdurable/dex-connectors-library/connectors/youdao/go/youdaomock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTextTranslationFlowIdentityAndMapping(t *testing.T) {
	input := youdao.TranslateTextInput{Text: "Hello", SourceLanguage: "en", TargetLanguage: "zh-CHS"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(youdao.Connection{})))
	require.Equal(t, "StartTranslateText", dex.GetFinalStepType[youdao.TranslateTextInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := youdaomock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
