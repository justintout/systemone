// Package clef is a client for Cloudflare's Clef models on Workers AI. Clef
// answers the System One API's questions, so questions and answers are the
// ones package systemone defines, and it also reads images placed before the
// state. See https://developers.cloudflare.com/workers-ai/models/clef.
//
//	client, err := clef.New(accountID, systemone.WithAPIKey(os.Getenv("CLOUDFLARE_AUTH_TOKEN")))
//	res, err := client.Ask(ctx, ticket, team, urgent)
//	t, err := team.From(res)
//
// A fine-tuned or self-hosted Clef is the same client pointed elsewhere with
// [systemone.WithBaseURL] and [systemone.WithEvaluatePath].
package clef

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/justintout/systemone"
	"github.com/justintout/systemone/internal/bridge"
)

// The models Cloudflare serves.
const (
	Model = "clef"
	Flash = "clef-flash"
)

// Client sends requests to Clef. It is safe for concurrent use.
type Client struct {
	base *systemone.Client
}

// New builds a client for the Workers AI account with the given ID. The API
// key is a Workers AI token, not a TypeSafe one, so pass it with
// [systemone.WithAPIKey].
//
// Clef names the model in the URL, wraps the payload in Cloudflare's envelope,
// and publishes no System One model listing, so [systemone.Client.Models] has
// no counterpart here.
func New(accountID string, opts ...systemone.ClientOption) (*Client, error) {
	if accountID == "" {
		return nil, fmt.Errorf("clef: no account ID")
	}
	defaults := []systemone.ClientOption{
		systemone.WithBaseURL("https://api.cloudflare.com"),
		systemone.WithModel(Model),
		systemone.WithEvaluatePath("/client/v4/accounts/" + accountID + "/ai/run/@cf/cloudflare/" + systemone.ModelPlaceholder),
		systemone.WithModelsPath(""),
		systemone.WithResultKey("result"),
		systemone.WithRequestIDHeader("Cf-Ray"),
	}
	base, err := systemone.New(append(defaults, opts...)...)
	if err != nil {
		return nil, err
	}
	return &Client{base: base}, nil
}

// Model returns the model used for requests that do not name one.
func (c *Client) Model() string { return c.base.Model() }

// Request is one evaluation. Its fields other than Images mean what they do in
// [systemone.Request].
type Request struct {
	State     any
	Questions []systemone.Question
	// Images are read before the state.
	Images  []Image
	Model   string
	Header  http.Header
	Retry   *systemone.RetryPolicy
	Timeout time.Duration
}

// Ask evaluates state against questions using the client's default model.
func (c *Client) Ask(ctx context.Context, state any, questions ...systemone.Question) (*systemone.Response, error) {
	return c.Do(ctx, Request{State: state, Questions: questions})
}

// Do sends one evaluation request. A [systemone.Box] question fails it before
// it is sent, since Clef does not answer them.
func (c *Client) Do(ctx context.Context, req Request) (*systemone.Response, error) {
	for _, q := range req.Questions {
		if bridge.Kind(q) == "box" {
			return nil, &systemone.QuestionError{QuestionID: q.ID(), Err: errors.New("box questions are not answered by Clef")}
		}
	}
	var ext bridge.Extension
	if len(req.Images) > 0 {
		ext.Fields = map[string]any{"images": req.Images}
	}
	return bridge.Do[*systemone.Response](ctx, c.base, systemone.Request{
		State:     req.State,
		Questions: req.Questions,
		Model:     req.Model,
		Header:    req.Header,
		Retry:     req.Retry,
		Timeout:   req.Timeout,
	}, ext)
}

// Image is one image sent with the state, read before it.
//
// Cloudflare takes PNG, JPEG and WebP, at most four per request, each under
// 4 MiB and 16 megapixels, and rejects remote URLs. Those limits are the
// deployment's and are enforced there, not here.
type Image struct {
	// ContentType is image/png, image/jpeg or image/webp.
	ContentType string
	// Data is the raw image bytes, base64 encoded on the wire.
	Data []byte
}

// contentTypes are the types [NewImage] will detect. A type outside this set
// is an error rather than something to send and have rejected.
var contentTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
}

// NewImage reads the content type from the bytes themselves, so a file is one
// call:
//
//	data, err := os.ReadFile("checkout.png")
//	img, err := clef.NewImage(data)
//
// It fails, wrapping [systemone.ErrImage], on a format Clef does not read.
func NewImage(data []byte) (Image, error) {
	if len(data) == 0 {
		return Image{}, fmt.Errorf("%w: no data", systemone.ErrImage)
	}
	// DetectContentType sniffs the first 512 bytes and never fails, returning
	// application/octet-stream when it recognizes nothing.
	ct := http.DetectContentType(data)
	if !contentTypes[ct] {
		return Image{}, fmt.Errorf("%w: content type %s, want one of image/png, image/jpeg, image/webp", systemone.ErrImage, ct)
	}
	return Image{ContentType: ct, Data: data}, nil
}

// MarshalJSON writes the base64 object form the API documents. The wire format
// also allows a data URL, which carries the same two fields less legibly.
func (i Image) MarshalJSON() ([]byte, error) {
	if i.ContentType == "" {
		return nil, fmt.Errorf("%w: no content type", systemone.ErrImage)
	}
	if len(i.Data) == 0 {
		return nil, fmt.Errorf("%w: no data", systemone.ErrImage)
	}
	return json.Marshal(struct {
		ContentType string `json:"content_type"`
		Base64      string `json:"base64"`
	}{i.ContentType, base64.StdEncoding.EncodeToString(i.Data)})
}
