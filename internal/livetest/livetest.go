// Package livetest holds what the live API tests in packages jev and clef
// share. Those tests run with -tags live, before a release, and check that
// requests flow end to end and their answers decode. What the model answers is
// inference, so they do not assert on it.
package livetest

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"testing"
)

// Env returns a required environment variable. A missing one fails the test
// rather than skipping it, so a release cannot pass with its live checks
// silently off.
func Env(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Fatalf("%s is not set", name)
	}
	return v
}

// Image is an 800x600 PNG, white with a blue rectangle, inside every
// deployment's size limits.
func Image(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(320, 150, 560, 450), &image.Uniform{color.RGBA{30, 80, 220, 255}}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
