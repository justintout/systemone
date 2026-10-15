package systemone

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ModelPlaceholder is replaced with the model of the request in the path set
// by [WithEvaluatePath], for deployments that name the model in the URL.
const ModelPlaceholder = "{model}"

// ErrNoModelList is returned by [Client.Models] when the deployment has no
// model listing.
var ErrNoModelList = errors.New("systemone: deployment has no model listing")

// ErrImage is returned by packages jev and clef for an image they will not
// send: one with no data, or one in a format the deployment does not read.
var ErrImage = errors.New("systemone: image")

// evaluateEndpoint is the URL an evaluation of model is posted to.
func (c *Client) evaluateEndpoint(model string) string {
	path := strings.ReplaceAll(c.evaluatePath, ModelPlaceholder, model)
	return c.baseURL.JoinPath(path).String()
}

// unwrap takes the payload out of the deployment's envelope.
func unwrap(data []byte, key string) ([]byte, error) {
	if key == "" {
		return data, nil
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}
	payload, ok := body[key]
	if !ok {
		return nil, fmt.Errorf("systemone: response has no %q field", key)
	}
	return payload, nil
}
