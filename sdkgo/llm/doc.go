// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package llm is the provider-neutral text-generation contract that every lab
// connector's generateText Query implements, and the shared pipeline that
// runs it.
//
// A connector aliases the contract types, so its generated Result type is
// TextGenerationResult and one application Step can handle the Result of
// every lab's generateText:
//
//	type GenerateTextRequest = llm.TextGenerationRequest
//	type GenerateTextResponse = llm.TextGenerationResponse
//
// The connector writes no request pipeline. It describes its provider API as
// a WireFormat, either from a family package such as openaichat or as a
// native struct of functions in its own module, and builds one
// TextGenerationQuery per client. The Query validates the model, request,
// structured-output schema, and credential before any request, classifies
// every provider outcome into the six shared branches, heartbeats while the
// attempt is in flight, and never copies prompts, provider message text, or
// credentials into a Failure or Receipt. The fixture connector in
// sdkgo/integrationtest/fixturellm builds it in its constructor:
//
//	wireFormat, err := openaichat.NewWireFormat(&chatProfile)
//	if err != nil {
//		return nil, err
//	}
//	generateText, err := llm.NewTextGenerationQuery(&llm.TextGenerationQueryConfig{
//		Definition: GenerateTextDefinition, WireFormat: wireFormat,
//		BaseURL: config.Endpoint, ConnectionModel: config.Model,
//		HTTPClient: resolved.httpClient, RequestTimeout: requestTimeout,
//		ResolveCredential: func(call sdkgo.Call) (sdkgo.SecretString, error) {
//			credential, err := credentials.Resolve(call)
//			return credential.APIKey, err
//		},
//		MaxResponseBytes:    cmp.Or(config.MaxResponseBytes, defaultMaxResponseBytes),
//		MaxStreamEventBytes: maxStreamEventBytes,
//	})
//
// The connector's GenerateText method returns that Query, and its generated
// Step factory runs it. The manifest declares the operation as generateText,
// kind query, idempotency none, durability sync, progress text, and the
// branches returned by TextGenerationBranchDefinitions.
//
// # Evolving under minimal version selection
//
// WireFormat and RequestFeatures grow only by new fields. A request field
// added to TextGenerationRequest in a later release gets a RequestFeatures
// flag that existing wire formats leave false, so an older connector selects
// defect with zero provider requests instead of silently ignoring the field.
//
// This holds for family wire formats that live in sdkgo, such as openaichat,
// only because they never turn a new flag on by themselves: a family package
// declares a new flag only when a new Profile field, whose zero value leaves
// the flag off, opts in. A connector released against an older SDK has no
// such field set, so under minimal version selection it keeps rejecting the
// new request field even when it links a newer sdkgo. Exported constructors
// and fields are never removed or retyped in v0.
package llm
