package main

import (
	"bytes"
	"fmt"
	"net/http"
)

func ai_tagImage(ref string) {
	fmt.Println("Tagging image:", ref)
	jsonData := []byte(`{"url": "http://localhost:3000` + ref + `"}`)
	resp, err := http.Post("http://localhost:8081/process", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer resp.Body.Close()
	fmt.Println("Response Status:", resp.Status)
}
