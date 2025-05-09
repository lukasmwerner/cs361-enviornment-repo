package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime"
	"strings"

	"github.com/philippgille/chromem-go"
)

// Configuration
const (
	serverPort   = "8081"
	ollamaAPIURL = "http://localhost:11434/api/generate" // Default Ollama API URL
	ollamaModel  = "gemma3:4b"                           // Vision model in Ollama
)

// OllamaRequest represents the request structure for Ollama API
type OllamaRequest struct {
	Model  string   `json:"model"`
	Prompt string   `json:"prompt"`
	Images []string `json:"images"` // Base64 encoded images
	Stream bool     `json:"stream"`
}

// OllamaResponse represents the response structure from Ollama API
type OllamaResponse struct {
	Model    string `json:"model"`
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

func main() {
	_, col, err := Open("db")
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/process", handleProcessRequest(col))
	http.HandleFunc("/search", handleSearch(col))

	fmt.Printf("Server started on port %s\n", serverPort)
	log.Fatal(http.ListenAndServe(":"+serverPort, nil))
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func handleSearch(collection *chromem.Collection) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method is supported", http.StatusMethodNotAllowed)
			return
		}

		// Parse the request body
		var requestBody struct {
			Query string `json:"query"`
		}

		err := json.NewDecoder(r.Body).Decode(&requestBody)
		if err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		results, err := collection.Query(r.Context(), requestBody.Query, min(collection.Count(), 5), nil, nil)
		if err != nil {
			log.Fatal(err)
		}

		type Response struct {
			ID         string  `json:"id"`
			Similarity float32 `json:"similarity"`
		}

		responses := []Response{}

		for _, result := range results {
			fmt.Printf("%s %f\n", result.ID, result.Similarity)
			responses = append(responses, Response{
				ID:         result.ID,
				Similarity: result.Similarity,
			})
		}

		json.NewEncoder(w).Encode(responses)
	}
}

func handleProcessRequest(collection *chromem.Collection) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method is supported", http.StatusMethodNotAllowed)
			return
		}

		// Parse the request body
		var requestBody struct {
			URL string `json:"url"`
		}

		err := json.NewDecoder(r.Body).Decode(&requestBody)
		if err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Validate URL
		if requestBody.URL == "" {
			http.Error(w, "URL is required", http.StatusBadRequest)
			return
		}

		fmt.Printf("Processing URL: %s\n", requestBody.URL)

		// Download the image
		imageData, err := downloadImage(requestBody.URL)
		if err != nil {
			fmt.Printf("Error downloading image: %v\n", err)
			http.Error(w, fmt.Sprintf("Error downloading image: %v", err), http.StatusInternalServerError)
			return
		}

		// Process the image with Ollama
		description, err := describeImage(imageData)
		if err != nil {
			fmt.Printf("Error describing image: %v\n", err)
			http.Error(w, fmt.Sprintf("Error describing image: %v", err), http.StatusInternalServerError)
			return
		}

		err = collection.AddDocuments(context.TODO(), []chromem.Document{{
			ID: strings.ReplaceAll(requestBody.URL, "http://localhost:3000", ""),
			Metadata: map[string]string{
				"image": requestBody.URL,
			},
			Content: description,
		}}, runtime.NumCPU())

		fmt.Println("Document added successfully", description)
		w.WriteHeader(http.StatusNoContent)
	}
}

// downloadImage downloads an image from a URL
func downloadImage(url string) ([]byte, error) {
	// Make the HTTP request
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check if the response status code is OK
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status: %s", resp.Status)
	}

	// Read the response body
	return io.ReadAll(resp.Body)
}

// describeImage sends the image to Ollama API for description
func describeImage(imageData []byte) (string, error) {
	// Encode image to base64
	encodedImage := base64.StdEncoding.EncodeToString(imageData)

	// Create the request payload
	requestData := OllamaRequest{
		Model:  ollamaModel,
		Prompt: "Describe this image in detail",
		Images: []string{encodedImage},
		Stream: false,
	}

	requestJSON, err := json.Marshal(requestData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make the request to Ollama API
	resp, err := http.Post(ollamaAPIURL, "application/json", bytes.NewBuffer(requestJSON))
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check if the response status code is OK
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("bad API response status: %s, body: %s", resp.Status, string(bodyBytes))
	}

	// Parse the response
	var ollamaResp OllamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	return ollamaResp.Response, nil
}
