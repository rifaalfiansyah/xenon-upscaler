package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
)

//go:embed web/*
var webFiles embed.FS

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	flag.Parse()

	subFS, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatalf("failed to open embedded web assets: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(subFS)))

	fmt.Printf("Xenon Upscaler running at http://localhost%s\n", *addr)
	fmt.Println("All image upscaling runs 100% client-side in your browser via Go WebAssembly.")
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}
