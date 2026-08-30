package main

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeIconCropsAndEncodesPNG(t *testing.T) {
	path := filepath.Join(t.TempDir(), "icon.png")
	source := image.NewNRGBA(image.Rect(0, 0, 600, 800))
	for y := 0; y < 800; y++ {
		for x := 0; x < 600; x++ {
			source.Set(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(file, source, nil); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if err := normalizeIcon(path); err != nil {
		t.Fatal(err)
	}

	file, err = os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	result, err := png.Decode(file)
	if err != nil {
		t.Fatalf("result is not PNG: %v", err)
	}
	if got := result.Bounds().Size(); got != (image.Point{X: 300, Y: 300}) {
		t.Fatalf("size = %v, want 300x300", got)
	}
	if _, _, _, alpha := result.At(30, 150).RGBA(); alpha != 0 {
		t.Fatalf("left padding alpha = %d, want 0", alpha)
	}
	if _, _, _, alpha := result.At(150, 30).RGBA(); alpha != 0 {
		t.Fatalf("top padding alpha = %d, want 0", alpha)
	}
	if _, _, _, alpha := result.At(150, 150).RGBA(); alpha == 0 {
		t.Fatal("icon content is transparent")
	}
}
