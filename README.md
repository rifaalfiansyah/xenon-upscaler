
# Xenon Upscaler
Simple **100% client-side** image upscaler built with **Go + WebAssembly (WASM)**. All image decoding, resampling, sharpening, and PNG encoding run locally inside the browser—no images are uploaded to any server.

## Features

- **Scale factors**: `2x`, `3x`, `4x`
- **Algorithms**:
  - `Lanczos3` (crisp, best for photos)
  - `Bicubic / Catmull-Rom` (balanced)
  - `Bilinear` (fast & smooth)
  - `Nearest Neighbor` (pixel art)
- **Unsharp Mask Sharpening**: Adjustable `0%`–`100%`
- **Supported inputs**: PNG, JPEG, GIF (outputs PNG)

## Run

```bash
go run .
```

Then open http://localhost:8080 in your browser.

## Rebuild WebAssembly

If you modify `upscaler/` or `cmd/wasm/`:

```powershell
$env:GOOS="js"; $env:GOARCH="wasm"; go build -o web/main.wasm ./cmd/wasm; Remove-Item Env:GOOS, Env:GOARCH
```

## Run Tests

```bash
go test ./...
```
