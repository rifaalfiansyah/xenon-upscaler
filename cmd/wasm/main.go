//go:build js && wasm

package main

import (
	"syscall/js"
	"time"

	"xenon-upscaler/upscaler"
)

func main() {
	js.Global().Set("upscaleImage", js.FuncOf(upscaleImageJS))
	js.Global().Set("xenonReady", js.ValueOf(true))
	select {}
}

func upscaleImageJS(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return errorResult("missing image bytes")
	}

	jsBytes := args[0]
	length := jsBytes.Get("byteLength").Int()
	input := make([]byte, length)
	js.CopyBytesToGo(input, jsBytes)

	scale := 2.0
	if len(args) >= 2 && args[1].Type() == js.TypeNumber {
		scale = args[1].Float()
	}

	algo := upscaler.Lanczos
	if len(args) >= 3 && args[2].Type() == js.TypeString {
		algo = upscaler.Algorithm(args[2].String())
	}

	sharpen := 0.2
	if len(args) >= 4 && args[3].Type() == js.TypeNumber {
		sharpen = args[3].Float()
	}

	format := upscaler.FormatPNG
	if len(args) >= 5 && args[4].Type() == js.TypeString {
		format = upscaler.Format(args[4].String())
	}

	quality := 90
	if len(args) >= 6 && args[5].Type() == js.TypeNumber {
		quality = args[5].Int()
	}

	start := time.Now()
	res, err := upscaler.UpscaleBytes(input, upscaler.Options{
		Scale:     scale,
		Algorithm: algo,
		Sharpen:   sharpen,
		Format:    format,
		Quality:   quality,
	})
	if err != nil {
		return errorResult(err.Error())
	}
	elapsedMs := float64(time.Since(start).Microseconds()) / 1000.0

	outArray := js.Global().Get("Uint8Array").New(len(res.Data))
	js.CopyBytesToJS(outArray, res.Data)

	obj := js.Global().Get("Object").New()
	obj.Set("data", outArray)
	obj.Set("width", res.Width)
	obj.Set("height", res.Height)
	obj.Set("origWidth", res.OrigWidth)
	obj.Set("origHeight", res.OrigHeight)
	obj.Set("format", string(res.Format))
	obj.Set("durationMs", elapsedMs)
	return obj
}

func errorResult(msg string) js.Value {
	obj := js.Global().Get("Object").New()
	obj.Set("error", msg)
	return obj
}
