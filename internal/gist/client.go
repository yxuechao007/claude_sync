package gist

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	apiBaseURL = "https://api.github.com"
)

// Client is a GitHub Gist API client
type Client struct {
	token      string
	httpClient *http.Client
}

// GistFile represents a file in a gist
type GistFile struct {
	Filename string `json:"filename,omitempty"`
	Type     string `json:"type,omitempty"`
	Language string `json:"language,omitempty"`
	RawURL   string `json:"raw_url,omitempty"`
	Size     int    `json:"size,omitempty"`
	Content  string `json:"content,omitempty"`
}

// Gist represents a GitHub Gist
type Gist struct {
	ID          string              `json:"id,omitempty"`
	URL         string              `json:"url,omitempty"`
	HTMLURL     string              `json:"html_url,omitempty"`
	Description string              `json:"description,omitempty"`
	Public      bool                `json:"public"`
	Files       map[string]GistFile `json:"files"`
	CreatedAt   time.Time           `json:"created_at,omitempty"`
	UpdatedAt   time.Time           `json:"updated_at,omitempty"`
}

// CreateGistRequest is the request body for creating a gist
type CreateGistRequest struct {
	Description string              `json:"description"`
	Public      bool                `json:"public"`
	Files       map[string]GistFile `json:"files"`
}

// UpdateGistRequest is the request body for updating a gist
type UpdateGistRequest struct {
	Description string              `json:"description,omitempty"`
	Files       map[string]GistFile `json:"files"`
}

// NewClient creates a new Gist API client
func NewClient(token string) *Client {
	return &Client{
		token: token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// doRequest performs an HTTP request with authentication
func (c *Client) doRequest(method, url string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// Create creates a new gist
func (c *Client) Create(description string, public bool, files map[string]string) (*Gist, error) {
	gistFiles := make(map[string]GistFile)
	for name, content := range files {
		gistFiles[name] = GistFile{Content: content}
	}

	reqBody := CreateGistRequest{
		Description: description,
		Public:      public,
		Files:       gistFiles,
	}

	resp, err := c.doRequest("POST", apiBaseURL+"/gists", reqBody)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to create gist: %s - %s", resp.Status, string(body))
	}

	var gist Gist
	if err := json.NewDecoder(resp.Body).Decode(&gist); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &gist, nil
}

// Get retrieves a gist by ID
func (c *Client) Get(gistID string) (*Gist, error) {
	resp, err := c.doRequest("GET", apiBaseURL+"/gists/"+gistID, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("gist not found: %s", gistID)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get gist: %s - %s", resp.Status, string(body))
	}

	var gist Gist
	if err := json.NewDecoder(resp.Body).Decode(&gist); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &gist, nil
}

// Update updates an existing gist
func (c *Client) Update(gistID string, files map[string]string) (*Gist, error) {
	gistFiles := make(map[string]GistFile)
	for name, content := range files {
		if content == "" {
			// Empty content means delete the file
			gistFiles[name] = GistFile{}
		} else {
			gistFiles[name] = GistFile{Content: content}
		}
	}

	reqBody := UpdateGistRequest{
		Files: gistFiles,
	}

	resp, err := c.doRequest("PATCH", apiBaseURL+"/gists/"+gistID, reqBody)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to update gist: %s - %s", resp.Status, string(body))
	}

	var gist Gist
	if err := json.NewDecoder(resp.Body).Decode(&gist); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &gist, nil
}

// Delete deletes a gist
func (c *Client) Delete(gistID string) error {
	resp, err := c.doRequest("DELETE", apiBaseURL+"/gists/"+gistID, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete gist: %s - %s", resp.Status, string(body))
	}

	return nil
}

// GetFileContent retrieves the content of a specific file from a gist
func (c *Client) GetFileContent(gistID, filename string) (string, error) {
	gist, err := c.Get(gistID)
	if err != nil {
		return "", err
	}

	file, ok := gist.Files[filename]
	if !ok {
		return "", fmt.Errorf("file not found in gist: %s", filename)
	}

	// If content is available directly, return it
	if file.Content != "" {
		return file.Content, nil
	}

	// Otherwise fetch from raw URL
	if file.RawURL != "" {
		resp, err := c.httpClient.Get(file.RawURL)
		if err != nil {
			return "", fmt.Errorf("failed to fetch file content: %w", err)
		}
		defer resp.Body.Close()

		content, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read file content: %w", err)
		}
		return string(content), nil
	}

	return "", fmt.Errorf("no content available for file: %s", filename)
}

// UpdateFile updates a single file in a gist
func (c *Client) UpdateFile(gistID, filename, content string) error {
	_, err := c.Update(gistID, map[string]string{filename: content})
	return err
}

// DeleteFile deletes a single file from a gist
func (c *Client) DeleteFile(gistID, filename string) error {
	_, err := c.Update(gistID, map[string]string{filename: ""})
	return err
}

// ListFiles returns a list of filenames in a gist
func (c *Client) ListFiles(gistID string) ([]string, error) {
	gist, err := c.Get(gistID)
	if err != nil {
		return nil, err
	}

	var files []string
	for name := range gist.Files {
		files = append(files, name)
	}
	return files, nil
}
