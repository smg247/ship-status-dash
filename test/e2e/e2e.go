package e2e

import (
	"bytes"
	"encoding/base64"
	"net/http"
)

// HTTP client helper with HTTP Basic Auth for mock oauth-proxy
type TestHTTPClient struct {
	publicURL    string
	protectedURL string
	client       *http.Client
	username     string
	password     string
}

func NewTestHTTPClient(publicURL string, protectedURL string) (*TestHTTPClient, error) {
	return NewTestHTTPClientWithUsername(publicURL, protectedURL, "developer")
}

func NewTestHTTPClientWithUsername(publicURL string, protectedURL string, username string) (*TestHTTPClient, error) {
	return &TestHTTPClient{
		publicURL:    publicURL,
		protectedURL: protectedURL,
		client:       &http.Client{},
		username:     username,
		password:     "developer",
	}, nil
}

func (c *TestHTTPClient) setAuthHeader(req *http.Request) {
	if c.username != "" && c.password != "" {
		auth := c.username + ":" + c.password
		encoded := base64.StdEncoding.EncodeToString([]byte(auth))
		req.Header.Set("Authorization", "Basic "+encoded)
	}
}

func (c *TestHTTPClient) Get(url string, protected bool) (*http.Response, error) {
	fullURL := c.publicURL + url
	if protected {
		fullURL = c.protectedURL + url
	}
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, err
	}
	if protected {
		c.setAuthHeader(req)
	}
	return c.client.Do(req)
}

func (c *TestHTTPClient) Post(url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest("POST", c.protectedURL+url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuthHeader(req)
	return c.client.Do(req)
}

func (c *TestHTTPClient) Put(url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest("PUT", c.protectedURL+url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuthHeader(req)
	return c.client.Do(req)
}

func (c *TestHTTPClient) Patch(url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest("PATCH", c.protectedURL+url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuthHeader(req)
	return c.client.Do(req)
}

func (c *TestHTTPClient) Delete(url string) (*http.Response, error) {
	req, err := http.NewRequest("DELETE", c.protectedURL+url, nil)
	if err != nil {
		return nil, err
	}
	c.setAuthHeader(req)
	return c.client.Do(req)
}

func (c *TestHTTPClient) PostWithBearerToken(url string, body []byte, token string, actingFor ...string) (*http.Response, error) {
	req, err := http.NewRequest("POST", c.protectedURL+url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if len(actingFor) > 0 && actingFor[0] != "" {
		req.Header.Set("X-Acting-For", actingFor[0])
	}
	return c.client.Do(req)
}

func (c *TestHTTPClient) PutWithBearerToken(url string, body []byte, token string, actingFor ...string) (*http.Response, error) {
	req, err := http.NewRequest("PUT", c.protectedURL+url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if len(actingFor) > 0 && actingFor[0] != "" {
		req.Header.Set("X-Acting-For", actingFor[0])
	}
	return c.client.Do(req)
}

func (c *TestHTTPClient) DeleteWithBearerToken(url string, token string, actingFor ...string) (*http.Response, error) {
	req, err := http.NewRequest("DELETE", c.protectedURL+url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if len(actingFor) > 0 && actingFor[0] != "" {
		req.Header.Set("X-Acting-For", actingFor[0])
	}
	return c.client.Do(req)
}
