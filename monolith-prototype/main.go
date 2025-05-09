package main

import (
	"fmt"
	"net/http"
)

func main() {
	s := NewServer("users.db")

	s.Mux.Handle("/static/",
		http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	fmt.Println("listening on :8080")
	s.ListenAndServe(":8080")
}
