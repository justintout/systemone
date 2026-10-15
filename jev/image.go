package jev

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"

	"github.com/justintout/systemone"
	"github.com/justintout/systemone/internal/bridge"
)

// Image is a PNG or JPEG sent with a request. It goes anywhere text can: in
// the state, in a question's instructions, or in its criteria. Placed as a
// value it encodes as its reference, which the model receives as the image;
// [Image.Ref] writes it into a sentence. Every image placed in a request must
// also be listed in [Request.Images], which uploads it once however often it
// is referenced.
//
//	door, err := jev.NewImage(doorJPG)
//	badge, err := jev.NewImage(badgeJPG)
//	match := systemone.NewNoul("match",
//		"Is the person in `door` the person in this badge photo: "+badge.Ref()+"?")
//	res, err := client.Do(ctx, jev.Request{
//		State:     map[string]any{"door": door},
//		Questions: []systemone.Question{match},
//		Images:    []jev.Image{door, badge},
//	})
//
// Images are experimental at TypeSafe, and the limits are theirs to change:
// 256 to 1568 pixels a side, 16 MiB a file, and still images only, upright,
// since the API ignores EXIF orientation. The deployment enforces them, not
// this package. See https://docs.typesafe.ai/images.
type Image struct {
	ref         string
	contentType string
	data        []byte
}

// contentTypes are the types [NewImage] accepts.
var contentTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
}

// NewImage reads the format from the bytes themselves and gives the image a
// new reference. It fails, wrapping [systemone.ErrImage], on anything but PNG
// or JPEG.
func NewImage(data []byte) (Image, error) {
	if len(data) == 0 {
		return Image{}, fmt.Errorf("%w: no data", systemone.ErrImage)
	}
	ct := http.DetectContentType(data)
	if !contentTypes[ct] {
		return Image{}, fmt.Errorf("%w: content type %s, want image/png or image/jpeg", systemone.ErrImage, ct)
	}
	// A reference is matched as plain text anywhere in the request, so it has
	// to be one no state or instruction could contain by accident.
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Image{}, err
	}
	return Image{ref: bridge.ImageRefPrefix + hex.EncodeToString(id[:]), contentType: ct, data: data}, nil
}

// Ref is the image's reference, to write into a longer string where the
// image belongs.
func (i Image) Ref() string { return i.ref }

// MarshalJSON writes the reference, so an Image placed as a value in the state,
// instructions or criteria becomes the image there.
func (i Image) MarshalJSON() ([]byte, error) {
	if i.ref == "" {
		return nil, fmt.Errorf("%w: not built with NewImage", systemone.ErrImage)
	}
	return json.Marshal(i.ref)
}

// asset is one entry of the request's assets field. The image travels as the
// multipart file part it names, which sends the bytes as they are rather than
// a third larger in base64.
type asset struct {
	Type string `json:"type"`
	Part string `json:"part"`
}

// assets builds the assets field from the listed images. Each image's part is
// named by its reference, which is already unique and never "request".
func assets(images []Image) (map[string]asset, error) {
	out := make(map[string]asset, len(images))
	for _, img := range images {
		if img.ref == "" {
			return nil, fmt.Errorf("%w: not built with NewImage", systemone.ErrImage)
		}
		if _, dup := out[img.ref]; dup {
			return nil, fmt.Errorf("%w: %s listed twice", systemone.ErrImage, img.ref)
		}
		out[img.ref] = asset{Type: "image", Part: img.ref}
	}
	return out, nil
}

// multipartBody sends the JSON request as the first part, named request, and
// each image as a file part after it.
func multipartBody(request []byte, images []Image) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="request"`},
		"Content-Type":        {"application/json"},
	})
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(request); err != nil {
		return nil, "", err
	}
	for _, img := range images {
		part, err := w.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": {fmt.Sprintf(`form-data; name=%q; filename=%q`, img.ref, img.ref)},
			"Content-Type":        {img.contentType},
		})
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(img.data); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// checkRefs matches the references in an encoded body against the listed
// images. The API rejects an image listed but never referenced; a reference
// to an image never listed it would pass to the model as plain text.
func checkRefs(body []byte, listed map[string]asset) error {
	const refLen = len(bridge.ImageRefPrefix) + 32
	seen := make(map[string]bool, len(listed))
	for rest := body; ; {
		i := bytes.Index(rest, []byte(bridge.ImageRefPrefix))
		if i < 0 {
			break
		}
		rest = rest[i:]
		if len(rest) < refLen {
			return fmt.Errorf("%w: truncated reference %q", systemone.ErrImage, rest)
		}
		ref := string(rest[:refLen])
		if _, ok := listed[ref]; !ok {
			return fmt.Errorf("%w: %s is placed in the request but not listed in Request.Images", systemone.ErrImage, ref)
		}
		seen[ref] = true
		rest = rest[refLen:]
	}
	for ref := range listed {
		if !seen[ref] {
			return fmt.Errorf("%w: %s is listed in Request.Images but placed nowhere in the request", systemone.ErrImage, ref)
		}
	}
	return nil
}
