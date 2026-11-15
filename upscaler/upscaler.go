package upscaler

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"math"

	_ "golang.org/x/image/webp"
)

// Algorithm represents an image resampling algorithm.
type Algorithm string

const (
	Nearest  Algorithm = "nearest"
	Bilinear Algorithm = "bilinear"
	Bicubic  Algorithm = "bicubic"
	Lanczos  Algorithm = "lanczos"
)

// Format represents the encoded output image format.
type Format string

const (
	FormatPNG  Format = "png"
	FormatJPEG Format = "jpeg"
)

const (
	// MaxInputBytes caps encoded input size (50 MiB) to protect browser memory.
	MaxInputBytes = 50 << 20
	// MaxOutputDimension caps width/height to prevent browser WASM OOM.
	MaxOutputDimension = 8192
	// MaxOutputPixels caps total output pixels (~33.5MP) to keep WASM heap safe.
	MaxOutputPixels = 8192 * 4096
)

// Options configures the upscaling operation.
type Options struct {
	Scale     float64
	Algorithm Algorithm
	Sharpen   float64 // 0.0 (none) to 1.0 (strong)
	Format    Format  // "png" (default) or "jpeg"
	Quality   int     // JPEG quality 1-100 (default 90)
}

// Result holds the upscaled image and metadata.
type Result struct {
	Data       []byte
	Width      int
	Height     int
	OrigWidth  int
	OrigHeight int
	Format     Format
}

func validateDimensions(w, h int, label string) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("%s has zero dimensions", label)
	}
	if w > MaxOutputDimension || h > MaxOutputDimension {
		return fmt.Errorf("%s dimensions %dx%d exceed maximum %dpx per side", label, w, h, MaxOutputDimension)
	}
	if int64(w)*int64(h) > MaxOutputPixels {
		return fmt.Errorf("%s resolution %dx%d exceeds maximum %d pixels", label, w, h, MaxOutputPixels)
	}
	return nil
}

// UpscaleBytes decodes an image from raw bytes, upscales it, and encodes it in the requested format.
func UpscaleBytes(input []byte, opts Options) (*Result, error) {
	if len(input) == 0 {
		return nil, errors.New("empty image input")
	}
	if len(input) > MaxInputBytes {
		return nil, fmt.Errorf("input size %d bytes exceeds maximum %d bytes", len(input), MaxInputBytes)
	}

	// Inspect header dimensions before full decode to guard against decompression bombs.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(input))
	if err != nil {
		return nil, fmt.Errorf("decode image config: %w", err)
	}
	if err := validateDimensions(cfg.Width, cfg.Height, "source"); err != nil {
		return nil, err
	}

	src, _, err := image.Decode(bytes.NewReader(input))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	dst, err := Upscale(src, opts)
	if err != nil {
		return nil, err
	}

	format := opts.Format
	if format == "" {
		format = FormatPNG
	}

	var data []byte
	switch format {
	case FormatPNG:
		var buf bytes.Buffer
		if err := png.Encode(&buf, dst); err != nil {
			return nil, fmt.Errorf("encode png: %w", err)
		}
		data = buf.Bytes()
	case FormatJPEG, "jpg":
		format = FormatJPEG
		quality := opts.Quality
		if quality <= 0 {
			quality = 90
		}
		quality = clamp(quality, 1, 100)
		data, err = EncodeJPEG(dst, quality)
		if err != nil {
			return nil, fmt.Errorf("encode jpeg: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported output format: %q", opts.Format)
	}

	srcBounds := src.Bounds()
	dstBounds := dst.Bounds()

	return &Result{
		Data:       data,
		Width:      dstBounds.Dx(),
		Height:     dstBounds.Dy(),
		OrigWidth:  srcBounds.Dx(),
		OrigHeight: srcBounds.Dy(),
		Format:     format,
	}, nil
}

// EncodeJPEG encodes an image as JPEG with the given quality (1-100).
func EncodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Upscale resizes src according to opts and returns a new *image.NRGBA image.
func Upscale(src image.Image, opts Options) (*image.NRGBA, error) {
	if src == nil {
		return nil, errors.New("source image is nil")
	}
	if opts.Scale <= 0 || opts.Scale > 8 {
		return nil, fmt.Errorf("scale must be between 0 and 8, got %v", opts.Scale)
	}

	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return nil, errors.New("source image has zero dimensions")
	}

	dstW := int(math.Round(float64(srcW) * opts.Scale))
	dstH := int(math.Round(float64(srcH) * opts.Scale))
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}
	if err := validateDimensions(dstW, dstH, "output"); err != nil {
		return nil, err
	}

	srcRGBA := toNRGBA(src)

	var dst *image.NRGBA
	switch opts.Algorithm {
	case Nearest:
		dst = resizeNearest(srcRGBA, dstW, dstH)
	case Bilinear:
		dst = resizeBilinear(srcRGBA, dstW, dstH)
	case Bicubic, "":
		dst = resizeKernel(srcRGBA, dstW, dstH, 2.0, catmullRom)
	case Lanczos:
		dst = resizeKernel(srcRGBA, dstW, dstH, 3.0, lanczos3)
	default:
		return nil, fmt.Errorf("unsupported algorithm: %q", opts.Algorithm)
	}

	if opts.Sharpen > 0 {
		dst = sharpenUnsharpMask(dst, clamp(opts.Sharpen, 0, 1))
	}

	return dst, nil
}

func toNRGBA(img image.Image) *image.NRGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if nrgba, ok := img.(*image.NRGBA); ok && b.Min == (image.Point{}) {
		return nrgba
	}

	dst := image.NewNRGBA(image.Rect(0, 0, w, h))

	switch src := img.(type) {
	case *image.RGBA:
		for y := 0; y < h; y++ {
			srcRow := (b.Min.Y+y-src.Rect.Min.Y)*src.Stride + (b.Min.X-src.Rect.Min.X)*4
			dstRow := y * dst.Stride
			for x := 0; x < w; x++ {
				sOff := srcRow + x*4
				dOff := dstRow + x*4
				r, g, bl, a := src.Pix[sOff+0], src.Pix[sOff+1], src.Pix[sOff+2], src.Pix[sOff+3]
				switch a {
				case 0:
					// zeroed by NewNRGBA
				case 255:
					dst.Pix[dOff+0] = r
					dst.Pix[dOff+1] = g
					dst.Pix[dOff+2] = bl
					dst.Pix[dOff+3] = 255
				default:
					a32 := uint32(a)
					dst.Pix[dOff+0] = uint8((uint32(r)*255 + a32/2) / a32)
					dst.Pix[dOff+1] = uint8((uint32(g)*255 + a32/2) / a32)
					dst.Pix[dOff+2] = uint8((uint32(bl)*255 + a32/2) / a32)
					dst.Pix[dOff+3] = a
				}
			}
		}
		return dst

	case *image.YCbCr:
		for y := 0; y < h; y++ {
			sy := b.Min.Y + y
			dstRow := y * dst.Stride
			for x := 0; x < w; x++ {
				sx := b.Min.X + x
				yi := src.YOffset(sx, sy)
				ci := src.COffset(sx, sy)
				r, g, bl := color.YCbCrToRGB(src.Y[yi], src.Cb[ci], src.Cr[ci])
				dOff := dstRow + x*4
				dst.Pix[dOff+0] = r
				dst.Pix[dOff+1] = g
				dst.Pix[dOff+2] = bl
				dst.Pix[dOff+3] = 255
			}
		}
		return dst
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(x, y, color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)))
		}
	}
	return dst
}

func resizeNearest(src *image.NRGBA, dstW, dstH int) *image.NRGBA {
	srcW := src.Bounds().Dx()
	srcH := src.Bounds().Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))

	xRatio := float64(srcW) / float64(dstW)
	yRatio := float64(srcH) / float64(dstH)

	for y := 0; y < dstH; y++ {
		sy := clamp(int(float64(y)*yRatio), 0, srcH-1)
		for x := 0; x < dstW; x++ {
			sx := clamp(int(float64(x)*xRatio), 0, srcW-1)
			srcOff := sy*src.Stride + sx*4
			dstOff := y*dst.Stride + x*4
			copy(dst.Pix[dstOff:dstOff+4], src.Pix[srcOff:srcOff+4])
		}
	}
	return dst
}

func resizeBilinear(src *image.NRGBA, dstW, dstH int) *image.NRGBA {
	srcW := src.Bounds().Dx()
	srcH := src.Bounds().Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))

	xScale := float64(srcW) / float64(dstW)
	yScale := float64(srcH) / float64(dstH)

	for y := 0; y < dstH; y++ {
		fy := (float64(y)+0.5)*yScale - 0.5
		y0 := int(math.Floor(fy))
		y1 := y0 + 1
		wy := fy - float64(y0)
		y0 = clamp(y0, 0, srcH-1)
		y1 = clamp(y1, 0, srcH-1)

		for x := 0; x < dstW; x++ {
			fx := (float64(x)+0.5)*xScale - 0.5
			x0 := int(math.Floor(fx))
			x1 := x0 + 1
			wx := fx - float64(x0)
			x0 = clamp(x0, 0, srcW-1)
			x1 = clamp(x1, 0, srcW-1)

			o00 := y0*src.Stride + x0*4
			o10 := y0*src.Stride + x1*4
			o01 := y1*src.Stride + x0*4
			o11 := y1*src.Stride + x1*4
			dstOff := y*dst.Stride + x*4

			w00 := (1 - wx) * (1 - wy)
			w10 := wx * (1 - wy)
			w01 := (1 - wx) * wy
			w11 := wx * wy

			for c := 0; c < 4; c++ {
				val := float64(src.Pix[o00+c])*w00 +
					float64(src.Pix[o10+c])*w10 +
					float64(src.Pix[o01+c])*w01 +
					float64(src.Pix[o11+c])*w11
				dst.Pix[dstOff+c] = clampUint8(val)
			}
		}
	}
	return dst
}

type kernelContrib struct {
	idx    int
	weight float32
}

func buildContribs(srcSize, dstSize int, radius float64, kernel func(float64) float64) [][]kernelContrib {
	scale := float64(srcSize) / float64(dstSize)
	contribs := make([][]kernelContrib, dstSize)
	for i := 0; i < dstSize; i++ {
		center := (float64(i)+0.5)*scale - 0.5
		left := int(math.Floor(center - radius))
		right := int(math.Ceil(center + radius))
		var list []kernelContrib
		var sum float64
		for s := left; s <= right; s++ {
			w := kernel(center - float64(s))
			if w != 0 {
				list = append(list, kernelContrib{idx: clamp(s, 0, srcSize-1), weight: float32(w)})
				sum += w
			}
		}
		if sum != 0 {
			invSum := float32(1.0 / sum)
			for j := range list {
				list[j].weight *= invSum
			}
		}
		contribs[i] = list
	}
	return contribs
}

// resizeKernel performs separable 2-pass (horizontal then vertical) kernel resampling.
func resizeKernel(src *image.NRGBA, dstW, dstH int, radius float64, kernel func(float64) float64) *image.NRGBA {
	srcW := src.Bounds().Dx()
	srcH := src.Bounds().Dy()

	// Horizontal pass: src (srcW x srcH) -> tmp (dstW x srcH) stored as float32 RGBA
	tmp := make([]float32, dstW*srcH*4)
	hContribs := buildContribs(srcW, dstW, radius, kernel)

	for y := 0; y < srcH; y++ {
		rowOff := y * src.Stride
		tmpRowOff := y * dstW * 4
		for x := 0; x < dstW; x++ {
			var r, g, b, a float32
			for _, c := range hContribs[x] {
				off := rowOff + c.idx*4
				r += float32(src.Pix[off+0]) * c.weight
				g += float32(src.Pix[off+1]) * c.weight
				b += float32(src.Pix[off+2]) * c.weight
				a += float32(src.Pix[off+3]) * c.weight
			}
			dstOff := tmpRowOff + x*4
			tmp[dstOff+0] = r
			tmp[dstOff+1] = g
			tmp[dstOff+2] = b
			tmp[dstOff+3] = a
		}
	}

	// Vertical pass: tmp (dstW x srcH) -> dst (dstW x dstH) using a cache-friendly row accumulator
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	vContribs := buildContribs(srcH, dstH, radius, kernel)
	rowBuf := make([]float32, dstW*4)
	rowLen := dstW * 4

	for y := 0; y < dstH; y++ {
		clear(rowBuf)
		for _, c := range vContribs[y] {
			srcRow := tmp[c.idx*rowLen : (c.idx+1)*rowLen]
			w := c.weight
			for i := 0; i < rowLen; i++ {
				rowBuf[i] += srcRow[i] * w
			}
		}
		dstRow := dst.Pix[y*dst.Stride : y*dst.Stride+rowLen]
		for i := 0; i < rowLen; i++ {
			dstRow[i] = clampUint8(float64(rowBuf[i]))
		}
	}

	return dst
}

// catmullRom evaluates the Catmull-Rom cubic spline kernel (a = -0.5).
func catmullRom(x float64) float64 {
	x = math.Abs(x)
	if x < 1 {
		return (1.5*x-2.5)*x*x + 1.0
	}
	if x < 2 {
		return ((-0.5*x+2.5)*x-4.0)*x + 2.0
	}
	return 0
}

// lanczos3 evaluates the Lanczos kernel with a = 3.
func lanczos3(x float64) float64 {
	if x == 0 {
		return 1
	}
	x = math.Abs(x)
	if x >= 3 {
		return 0
	}
	px := math.Pi * x
	return (math.Sin(px) / px) * (math.Sin(px/3) / (px / 3))
}

// sharpenUnsharpMask applies a 3x3 unsharp mask to RGB channels while preserving alpha.
func sharpenUnsharpMask(src *image.NRGBA, strength float64) *image.NRGBA {
	w := src.Bounds().Dx()
	h := src.Bounds().Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	amount := strength * 1.2

	for y := 0; y < h; y++ {
		yPrev := clamp(y-1, 0, h-1)
		yNext := clamp(y+1, 0, h-1)
		for x := 0; x < w; x++ {
			xPrev := clamp(x-1, 0, w-1)
			xNext := clamp(x+1, 0, w-1)

			cOff := y*src.Stride + x*4
			nOff := yPrev*src.Stride + x*4
			sOff := yNext*src.Stride + x*4
			wOff := y*src.Stride + xPrev*4
			eOff := y*src.Stride + xNext*4

			dstOff := y*dst.Stride + x*4
			for c := 0; c < 3; c++ {
				center := float64(src.Pix[cOff+c])
				blur := (center*4 +
					float64(src.Pix[nOff+c]) +
					float64(src.Pix[sOff+c]) +
					float64(src.Pix[wOff+c]) +
					float64(src.Pix[eOff+c])) / 8.0
				sharpened := center + amount*(center-blur)
				dst.Pix[dstOff+c] = clampUint8(sharpened)
			}
			dst.Pix[dstOff+3] = src.Pix[cOff+3]
		}
	}
	return dst
}

func clamp[T int | float64](v, min, max T) T {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func clampUint8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(math.Round(v))
}
