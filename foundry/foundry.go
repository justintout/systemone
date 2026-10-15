// Package foundry reaches Microsoft-Decision-1 deployed in Microsoft Foundry.
// Foundry serves the base System One API, so the client is a
// [systemone.Client] and questions and answers are the ones package systemone
// defines. See
// https://learn.microsoft.com/azure/foundry/foundry-models/how-to/use-foundry-models-microsoft-decision.
//
// Foundry takes a Microsoft Entra ID token as a bearer token, or a resource
// API key in the api-key header:
//
//	client, err := foundry.New(endpoint, deployment, systemone.WithAPIKey(entraToken))
//
//	client, err := foundry.New(endpoint, deployment,
//		systemone.WithAPIKey(key), systemone.WithAPIKeyHeader(foundry.APIKeyHeader))
//
// The client sends the token it was built with and never refreshes it, so this
// module does not depend on Azure's SDK. An Entra token expires, typically
// within an hour. A long-running process should use an API key, or get tokens
// for [TokenScope] from azidentity and build a new client before each one
// expires.
package foundry

import (
	"errors"

	"github.com/justintout/systemone"
)

// APIKeyHeader is the header Foundry reads a resource API key from.
const APIKeyHeader = "api-key"

// TokenScope is the scope an Entra ID token for Foundry is requested with.
const TokenScope = "https://cognitiveservices.azure.com/.default"

// New builds a client for the Foundry resource at endpoint, the resource
// endpoint on the deployment's details page, sending to the named deployment.
// Requests name the deployment, not the model, so [systemone.Request.Model]
// takes another deployment's name.
//
// Foundry publishes no System One model listing, so [systemone.Client.Models]
// returns [systemone.ErrNoModelList]. Microsoft does not document zstd request
// bodies, so compression is off.
func New(endpoint, deployment string, opts ...systemone.ClientOption) (*systemone.Client, error) {
	switch {
	case endpoint == "":
		return nil, errors.New("foundry: no endpoint")
	case deployment == "":
		return nil, errors.New("foundry: no deployment")
	}
	defaults := []systemone.ClientOption{
		systemone.WithBaseURL(endpoint),
		systemone.WithModel(deployment),
		systemone.WithEvaluatePath("/providers/microsoft/v1/systemone"),
		systemone.WithModelsPath(""),
	}
	return systemone.New(append(defaults, opts...)...)
}
