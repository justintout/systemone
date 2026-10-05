package systemone

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
)

// Image is one image sent with the state, read before it. Images are a Clef
// extension to the System One API, so a provider has to accept them; see
// [Provider.AcceptsImages].
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

// imageContentTypes are the types [NewImage] will detect. A type outside this
// set is an error rather than something to send and have rejected.
var imageContentTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
}

// NewImage reads the content type from the bytes themselves, so a file is one
// call:
//
//	data, err := os.ReadFile("checkout.png")
//	img, err := systemone.NewImage(data)
//
// It fails on a format Clef does not read, which is what distinguishes an
// image the deployment would reject from one it will take.
func NewImage(data []byte) (Image, error) {
	if len(data) == 0 {
		return Image{}, fmt.Errorf("%w: no data", ErrImage)
	}
	// DetectContentType sniffs the first 512 bytes and never fails, returning
	// application/octet-stream when it recognizes nothing.
	ct := http.DetectContentType(data)
	if !imageContentTypes[ct] {
		return Image{}, fmt.Errorf("%w: content type %s, want one of image/png, image/jpeg, image/webp", ErrImage, ct)
	}
	return Image{ContentType: ct, Data: data}, nil
}

// MarshalJSON writes the base64 object form the API documents. The wire format
// also allows a data URL, which carries the same two fields less legibly.
func (i Image) MarshalJSON() ([]byte, error) {
	if i.ContentType == "" {
		return nil, fmt.Errorf("%w: no content type", ErrImage)
	}
	if len(i.Data) == 0 {
		return nil, fmt.Errorf("%w: no data", ErrImage)
	}
	return json.Marshal(struct {
		ContentType string `json:"content_type"`
		Base64      string `json:"base64"`
	}{i.ContentType, base64.StdEncoding.EncodeToString(i.Data)})
}
