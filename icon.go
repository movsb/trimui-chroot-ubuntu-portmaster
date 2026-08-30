package main

import (
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

const nativeIconCanvasSize = 300
const nativeIconContentSize = 180

func normalizeIcon(path string) error {
	input, err := os.Open(path)
	if err != nil {
		return err
	}
	source, format, err := image.Decode(input)
	closeErr := input.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}

	bounds := source.Bounds()
	visible := visibleBounds(source)
	if bounds.Dx() == nativeIconCanvasSize && bounds.Dy() == nativeIconCanvasSize &&
		visible.Dx() <= nativeIconContentSize && visible.Dy() <= nativeIconContentSize && format == "png" {
		return nil
	}

	side := min(bounds.Dx(), bounds.Dy())
	left := bounds.Min.X + (bounds.Dx()-side)/2
	top := bounds.Min.Y + (bounds.Dy()-side)/2
	crop := image.Rect(left, top, left+side, top+side)
	destination := image.NewNRGBA(image.Rect(0, 0, nativeIconCanvasSize, nativeIconCanvasSize))
	padding := (nativeIconCanvasSize - nativeIconContentSize) / 2
	content := image.Rect(padding, padding, padding+nativeIconContentSize, padding+nativeIconContentSize)
	draw.CatmullRom.Scale(destination, content, source, crop, draw.Over, nil)

	temporary, err := os.CreateTemp(filepath.Dir(path), ".icon-*.png")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := png.Encode(temporary, destination); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporaryPath, 0o755); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace icon: %w", err)
	}
	return nil
}

func visibleBounds(source image.Image) image.Rectangle {
	bounds := source.Bounds()
	visible := image.Rectangle{}
	found := false
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := source.At(x, y).RGBA()
			if alpha == 0 {
				continue
			}
			if !found {
				visible = image.Rect(x, y, x+1, y+1)
				found = true
				continue
			}
			if x < visible.Min.X {
				visible.Min.X = x
			}
			if y < visible.Min.Y {
				visible.Min.Y = y
			}
			if x >= visible.Max.X {
				visible.Max.X = x + 1
			}
			if y >= visible.Max.Y {
				visible.Max.Y = y + 1
			}
		}
	}
	return visible
}
