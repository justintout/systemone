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
}

var do any

// RegisterDo installs package systemone's request path.
func RegisterDo[S, C, R any](f func(ctx context.Context, client C, req R, ext Extension) (S, error)) {
	do = f
}

// Do sends req through client with ext applied.
func Do[S, C, R any](ctx context.Context, client C, req R, ext Extension) (S, error) {
	return do.(func(context.Context, C, R, Extension) (S, error))(ctx, client, req, ext)
}
