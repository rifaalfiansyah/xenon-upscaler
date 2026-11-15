package upscaler

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func makeTestImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x * 40) % 256),
				G: uint8((y * 50) % 256),
				B: uint8(((x + y) * 30) % 256),
				A: 255,
			})
		}
	}
	return img
}

func TestUpscaleAlgorithms(t *testing.T) {
	src := makeTestImage(8, 6)
	algos := []Algorithm{Nearest, Bilinear, Bicubic, Lanczos}

	for _, algo := range algos {
		t.Run(string(algo), func(t *testing.T) {
			dst, err := Upscale(src, Options{
				Scale:     2,
				Algorithm: algo,
				Sharpen:   0.3,
			})
			if err != nil {
				t.Fatalf("Upscale(%s) failed: %v", algo, err)
			}
			if dst.Bounds().Dx() != 16 || dst.Bounds().Dy() != 12 {
				t.Fatalf("unexpected dimensions: got %dx%d, want 16x12", dst.Bounds().Dx(), dst.Bounds().Dy())
			}
		})
	}
}

func TestUpscaleBytesRoundTrip(t *testing.T) {
	src := makeTestImage(4, 4)
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatalf("encode test png: %v", err)
	}

	res, err := UpscaleBytes(buf.Bytes(), Options{
		Scale:     3,
		Algorithm: Lanczos,
		Sharpen:   0.2,
	})
	if err != nil {
		t.Fatalf("UpscaleBytes failed: %v", err)
	}
	if res.OrigWidth != 4 || res.OrigHeight != 4 {
		t.Fatalf("unexpected orig size: %dx%d", res.OrigWidth, res.OrigHeight)
	}
	if res.Width != 12 || res.Height != 12 {
		t.Fatalf("unexpected output size: %dx%d", res.Width, res.Height)
	}

	decoded, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatalf("decode output png: %v", err)
	}
	if decoded.Bounds().Dx() != 12 || decoded.Bounds().Dy() != 12 {
		t.Fatalf("decoded png size mismatch: %v", decoded.Bounds())
	}
}

func TestUpscaleValidation(t *testing.T) {
	src := makeTestImage(4, 4)
	if _, err := Upscale(nil, Options{Scale: 2}); err == nil {
		t.Fatal("expected error for nil image")
	}
	if _, err := Upscale(src, Options{Scale: 0}); err == nil {
		t.Fatal("expected error for scale 0")
	}
	if _, err := Upscale(src, Options{Scale: 10}); err == nil {
		t.Fatal("expected error for scale > 8")
	}
	if _, err := Upscale(src, Options{Scale: 2, Algorithm: "unknown"}); err == nil {
		t.Fatal("expected error for unknown algorithm")
	}
	if _, err := UpscaleBytes(nil, Options{Scale: 2}); err == nil {
		t.Fatal("expected error for empty bytes")
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, src)
	if _, err := UpscaleBytes(buf.Bytes(), Options{Scale: 2, Format: "bmp"}); err == nil {
		t.Fatal("expected error for unsupported output format")
	}
}

func TestUpscaleBytesJPEG(t *testing.T) {
	src := makeTestImage(8, 8)
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatalf("encode test png: %v", err)
	}

	res, err := UpscaleBytes(buf.Bytes(), Options{
		Scale:     2,
		Algorithm: Bicubic,
		Sharpen:   0.1,
		Format:    FormatJPEG,
		Quality:   85,
	})
	if err != nil {
		t.Fatalf("UpscaleBytes JPEG failed: %v", err)
	}
	if res.Format != FormatJPEG {
		t.Fatalf("unexpected format: got %q, want %q", res.Format, FormatJPEG)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatalf("decode output jpeg: %v", err)
	}
	if decoded.Bounds().Dx() != 16 || decoded.Bounds().Dy() != 16 {
		t.Fatalf("decoded jpeg size mismatch: %v", decoded.Bounds())
	}
}

func TestUpscaleDimensionGuard(t *testing.T) {
	// Zero-allocation test image via SubImage/NRGBA bounds check:
	// Use a 3000x10 image scaled by 3 -> 9000x30 (> MaxOutputDimension 8192)
	wide := image.NewNRGBA(image.Rect(0, 0, 3000, 10))
	if _, err := Upscale(wide, Options{Scale: 3, Algorithm: Nearest}); err == nil {
		t.Fatal("expected error when output dimension exceeds MaxOutputDimension")
	}

	// 6000x6000 scaled by 1.2 -> 7200x7200 = 51.84M pixels (> MaxOutputPixels)
	big := image.NewNRGBA(image.Rect(0, 0, 6000, 6000))
	if _, err := Upscale(big, Options{Scale: 1.2, Algorithm: Nearest}); err == nil {
		t.Fatal("expected error when output pixels exceed MaxOutputPixels")
	}
}

func TestToNRGBAFastPaths(t *testing.T) {
	// *image.RGBA fast path (opaque + semi-transparent + transparent)
	rgba := image.NewRGBA(image.Rect(0, 0, 4, 4))
	rgba.SetRGBA(0, 0, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	rgba.SetRGBA(1, 0, color.RGBA{R: 100, G: 50, B: 25, A: 128})
	rgba.SetRGBA(2, 0, color.RGBA{R: 0, G: 0, B: 0, A: 0})

	dstRGBA, err := Upscale(rgba, Options{Scale: 2, Algorithm: Bilinear})
	if err != nil {
		t.Fatalf("Upscale RGBA failed: %v", err)
	}
	if dstRGBA.Bounds().Dx() != 8 || dstRGBA.Bounds().Dy() != 8 {
		t.Fatalf("unexpected RGBA output size: %v", dstRGBA.Bounds())
	}

	// *image.YCbCr fast path
	ycbcr := image.NewYCbCr(image.Rect(0, 0, 4, 4), image.YCbCrSubsampleRatio420)
	for i := range ycbcr.Y {
		ycbcr.Y[i] = 180
	}
	for i := range ycbcr.Cb {
		ycbcr.Cb[i] = 128
		ycbcr.Cr[i] = 128
	}

	dstYCbCr, err := Upscale(ycbcr, Options{Scale: 2, Algorithm: Lanczos})
	if err != nil {
		t.Fatalf("Upscale YCbCr failed: %v", err)
	}
	if dstYCbCr.Bounds().Dx() != 8 || dstYCbCr.Bounds().Dy() != 8 {
		t.Fatalf("unexpected YCbCr output size: %v", dstYCbCr.Bounds())
	}
}
