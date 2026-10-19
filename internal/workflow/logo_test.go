package workflow

import (
	"image"
	"image/color"
	"testing"
)

func TestCropLogoRemovesWhiteSourcePage(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			source.Set(x, y, color.White)
		}
	}
	for y := 2; y < 8; y++ {
		for x := 3; x < 9; x++ {
			source.Set(x, y, color.Black)
		}
	}
	cropped := cropLogo(source)
	if got := cropped.Bounds(); got.Dx() != 6 || got.Dy() != 6 {
		t.Fatalf("cropped bounds = %v, want 6x6", got)
	}
	if _, _, _, alpha := cropped.At(0, 0).RGBA(); alpha == 0 {
		t.Fatal("source mark pixel unexpectedly became transparent")
	}
}
