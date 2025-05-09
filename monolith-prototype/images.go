package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

type ImageUploadImage struct {
	Id   string `json:"id"`
	Data string `json:"data"`
}

// ImageUploadResult contains the response and filename for an uploaded image
type ImageUploadResult struct {
	Filename string
	Response string
	URL      string
}

func stripBase64Prefix(base64String string) string {
	if idx := strings.Index(base64String, ";base64,"); idx != -1 {
		return base64String[idx+8:]
	}
	return base64String
}

func getExtensionFromDataURL(dataURL string) string {
	// Default extension if we can't determine it
	extension := "jpg"

	if strings.HasPrefix(dataURL, "data:") {
		// Extract the MIME type part
		parts := strings.Split(dataURL, ";")
		if len(parts) > 0 {
			mimeParts := strings.Split(parts[0], "/")
			if len(mimeParts) > 1 {
				mimeType := mimeParts[1]
				// Handle common image types
				switch mimeType {
				case "jpeg":
					extension = "jpg"
				case "png", "gif", "webp", "svg+xml":
					extension = strings.Replace(mimeType, "+xml", "", 1)
				default:
					extension = mimeType
				}
			}
		}
	}

	return extension
}

// uploadBase64Images uploads a slice of base64-encoded images to the specified URL
// Returns a slice of ImageUploadResults and any error that occurred
func uploadBase64Images(baseURL string, base64Images []ImageUploadImage) ([]ImageUploadResult, error) {
	var results []ImageUploadResult

	for i, base64Image := range base64Images {

		// Get file extension from the data URL
		extension := getExtensionFromDataURL(base64Image.Data)

		// Strip the prefix to get just the base64-encoded data
		base64Data := stripBase64Prefix(base64Image.Data)

		// Decode base64 string to bytes
		imageData, err := base64.StdEncoding.DecodeString(base64Data)
		if err != nil {
			return results, fmt.Errorf("error decoding base64 image %d: %w", i, err)
		}

		// Generate filename based on SHA-256 hash of the image data
		hasher := sha256.New()
		hasher.Write(imageData)
		hash := hex.EncodeToString(hasher.Sum(nil))
		filename := fmt.Sprintf("%s.%s", hash, extension)

		// Create a buffer to hold the multipart form data
		var requestBody bytes.Buffer
		multipartWriter := multipart.NewWriter(&requestBody)

		// Create a form file field with the filename
		part, err := multipartWriter.CreateFormFile("file", filename)
		if err != nil {
			return results, fmt.Errorf("error creating form file for %s: %w", filename, err)
		}

		// Write the image data to the form file field
		_, err = io.Copy(part, bytes.NewReader(imageData))
		if err != nil {
			return results, fmt.Errorf("error writing image data for %s: %w", filename, err)
		}

		// Close the multipart writer to finalize the form
		multipartWriter.Close()

		// Create the HTTP request
		uploadURL := fmt.Sprintf("%s/upload", baseURL)
		req, err := http.NewRequest("POST", uploadURL, &requestBody)
		if err != nil {
			return results, fmt.Errorf("error creating request for %s: %w", filename, err)
		}

		// Set the content type header with the boundary
		req.Header.Set("Content-Type", multipartWriter.FormDataContentType())

		// Send the request
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return results, fmt.Errorf("error sending request for %s: %w", filename, err)
		}

		// Read the response
		defer resp.Body.Close()
		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return results, fmt.Errorf("error reading response for %s: %w", filename, err)
		}

		// Calculate the final image URL
		imageURL := fmt.Sprintf("/images/%s", filename)

		// Add the result to our results
		results = append(results, ImageUploadResult{
			Filename: filename,
			Response: string(responseBody),
			URL:      imageURL,
		})
	}

	return results, nil
}
