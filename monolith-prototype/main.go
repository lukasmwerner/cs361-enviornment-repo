package main

import (
	"embed"
	"fmt"
	"net/http"
)

// go:embed static
var staticFS embed.FS

func main() {

	s := NewServer("users.db")

	s.Mux.Handle("/static", http.FileServer(http.FS(staticFS)))

	fmt.Println("listening on :8080")
	s.ListenAndServe(":8080")
}
