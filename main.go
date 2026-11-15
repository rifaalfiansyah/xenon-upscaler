package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"time"
)

//go:embed web/*
var webFiles embed.FS

const cspHeader = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval' https://cdn.jsdelivr.net; " +
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"font-src https://fonts.gstatic.com; " +
	"img-src 'self' blob:; " +
	"worker-src 'self'; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'"

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", cspHeader)
		next.ServeHTTP(w, r)
	})
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()

	subFS, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatalf("failed to open embedded web assets: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(subFS)))

	srv := &http.Server{
		Addr:              *addr,
		Handler:           withSecurityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	fmt.Printf("Xenon Upscaler running at http://%s\n", *addr)
	fmt.Println("All image upscaling runs 100% client-side in your browser via Go WebAssembly.")
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
