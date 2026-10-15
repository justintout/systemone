// Package bridge lets the jev and clef packages send requests through package
// systemone without the hooks they need becoming part of its public API.
// Package systemone registers the hooks in its init, which has always run by
// the time a package importing it calls one.
package bridge

import "context"

// Extension is what a deployment package adds to a request of the base API.
type Extension struct {
	// Fields are extra top-level fields of the request body.
	Fields map[string]any
	// Check inspects the encoded state and questions before Fields are added
	// and before anything is sent. Without one, a body carrying an
	// ImageRefPrefix is refused, since only a deployment that sends assets
	// can resolve it.
	Check func(body []byte) error
	// Wrap replaces the finished JSON body with the body to send and its
	// content type, such as a multipart body carrying the JSON as one part.
	Wrap func(body []byte) ([]byte, string, error)
}

// ImageRefPrefix starts every reference a jev.Image encodes as. It lets the
// client find references in an encoded body, so an image placed in a request
// but never attached fails before it is sent instead of reaching the model as
// a meaningless string.
const ImageRefPrefix = "systemone-image-"

// Kind returns the wire type of a systemone.Question, such as "box". Package
// systemone sets it.
var Kind func(question any) string

var do any

// RegisterDo installs package systemone's request path.
func RegisterDo[S, C, R any](f func(ctx context.Context, client C, req R, ext Extension) (S, error)) {
	do = f
}

// Do sends req through client with ext applied.
func Do[S, C, R any](ctx context.Context, client C, req R, ext Extension) (S, error) {
	return do.(func(context.Context, C, R, Extension) (S, error))(ctx, client, req, ext)
}
