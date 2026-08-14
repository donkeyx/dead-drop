// Package turnstile verifies Cloudflare Turnstile tokens (create-only human check).
package turnstile

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// Client calls Siteverify.
type Client struct {
	Secret    string
	VerifyURL string
	HTTP      *http.Client
}

type siteverifyResp struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
}

// Verify reports whether token is a fresh, valid Turnstile response.
func (c *Client) Verify(token, remoteIP string) error {
	if c == nil || c.Secret == "" {
		return nil
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("missing token")
	}
	endpoint := c.VerifyURL
	if endpoint == "" {
		endpoint = DefaultVerifyURL
	}
	form := url.Values{}
	form.Set("secret", c.Secret)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := httpClient.PostForm(endpoint, form)
	if err != nil {
		return fmt.Errorf("siteverify: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return fmt.Errorf("siteverify read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("siteverify status %s", resp.Status)
	}
	var out siteverifyResp
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("siteverify json: %w", err)
	}
	if !out.Success {
		if len(out.ErrorCodes) > 0 {
			return fmt.Errorf("siteverify: %s", strings.Join(out.ErrorCodes, ","))
		}
		return fmt.Errorf("siteverify rejected")
	}
	return nil
}
