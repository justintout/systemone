// Command locate finds something in a photo with a Box question, following
// https://docs.typesafe.ai/primitives/box, and saves a crop of it.
//
// A Box answer is always a box, even when the target is absent, so the same
// request asks a Noul whether the target is there at all. The photo goes in
// the state once and both questions refer to it by name.
//
// Set TYPESAFE_API_KEY, then:
//
//	go run ./examples/locate kitchen.jpg "the coffee mug"
//
// The photo must be a PNG or JPEG, upright, 256 to 1568 pixels a side.
package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // decode the photo to crop it
	"image/png"
	"log"
	"os"

	"github.com/justintout/systemone"
	"github.com/justintout/systemone/jev"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: locate PHOTO TARGET")
	}
	path, target := os.Args[1], os.Args[2]

	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	photo, err := jev.NewImage(data)
	if err != nil {
		log.Fatal(err)
	}

	present := systemone.NewNoul("present", fmt.Sprintf("Is %s in `photo`?", target))
	where := jev.NewBox("where", target+" in `photo`")

	client, err := jev.New()
	if err != nil {
		log.Fatal(err)
	}
	res, err := client.Do(context.Background(), jev.Request{
		State:     map[string]any{"photo": photo},
		Questions: []systemone.Question{present},
		Boxes:     []*jev.Box{where},
		Images:    []jev.Image{photo},
		Model:     jev.Preview, // Box needs Jev 1.14
	})
	if err != nil {
		log.Fatal(err)
	}

	p, err := present.From(res)
	if err != nil {
		log.Fatal(err)
	}
	b, err := where.From(res)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("answered by %s (%d input tokens)\n\n", res.Model, res.Usage.InputTokens)
	fmt.Printf("present: p=%.2f\n", p.Value)
	fmt.Printf("box:     top %.3f  bottom %.3f  left %.3f  right %.3f  confidence %.2f\n",
		b.Top, b.Bottom, b.Left, b.Right, b.Confidence)
	if !p.Yes(0.5) {
		fmt.Println("\nnot found, so the box means nothing")
		return
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		log.Fatal(err)
	}
	size := img.Bounds().Size()
	rect := b.Rect(size.X, size.Y).Add(img.Bounds().Min)
	fmt.Printf("pixels:  %v in a %dx%d photo\n", rect, size.X, size.Y)

	crop, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		log.Fatal("this image type cannot be cropped")
	}
	out, err := os.Create("crop.png")
	if err != nil {
		log.Fatal(err)
	}
	if err := png.Encode(out, crop.SubImage(rect)); err != nil {
		log.Fatal(err)
	}
	if err := out.Close(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("\nwrote crop.png")
}
