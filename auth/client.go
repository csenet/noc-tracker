package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	httpClient      *http.Client
	settings        *Settings
	username        string
	password        string
	token           *AuthToken
	tokenObtainedAt time.Time
	sessionToken    string
	pkceChallenge   *PKCEChallenge
}

func NewClient(username, password string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		username:   username,
		password:   password,
	}
}

func (c *Client) fetchSettings() error {
	resp, err := c.httpClient.Get("https://portal.arubainstanton.com/settings.json")
	if err != nil {
		return fmt.Errorf("fetch settings: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("fetch settings: status %d, body: %s", resp.StatusCode, string(body))
	}

	var settings Settings
	if err := json.NewDecoder(resp.Body).Decode(&settings); err != nil {
		return fmt.Errorf("decode settings: %w", err)
	}

	if settings.SSOBaseURL == "" && settings.SSOFQDN != "" {
		settings.SSOBaseURL = settings.SSOFQDN + "/as"
	}
	if settings.SSOEndpointAuthZ == "" {
		settings.SSOEndpointAuthZ = "/authorization.oauth2"
	}
	if settings.SSOEndpointTokens == "" {
		settings.SSOEndpointTokens = "/token.oauth2"
	}

	c.settings = &settings
	return nil
}

func (c *Client) getSessionToken() error {
	if c.settings == nil {
		if err := c.fetchSettings(); err != nil {
			return err
		}
	}

	data := url.Values{}
	data.Set("username", c.username)
	data.Set("password", c.password)

	req, err := http.NewRequest("POST", "https://sso.arubainstanton.com/aio/api/v1/mfa/validate/full", strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("MFA request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("MFA validation failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	var mfaResp AuthToken
	if err := json.Unmarshal(body, &mfaResp); err != nil {
		return fmt.Errorf("decode MFA: %w", err)
	}
	if !mfaResp.Success || mfaResp.AccessToken == "" {
		return fmt.Errorf("MFA validation did not return a session token")
	}

	c.sessionToken = mfaResp.AccessToken
	return nil
}

func (c *Client) getAuthorizationCode() (string, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return "", err
	}
	c.pkceChallenge = pkce

	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", c.settings.SSOClientIDAuthZ)
	params.Set("scope", "profile openid")
	params.Set("redirect_uri", "https://portal.arubainstanton.com")
	params.Set("code_challenge", pkce.Challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("sessionToken", c.sessionToken)

	authURL := fmt.Sprintf("%s%s?%s", c.settings.SSOBaseURL, c.settings.SSOEndpointAuthZ, params.Encode())

	noFollow := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := noFollow.Get(authURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	location := resp.Header.Get("Location")
	if location == "" {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("no redirect, status %d, body %s", resp.StatusCode, string(body))
	}
	u, err := url.Parse(location)
	if err != nil {
		return "", err
	}
	code := u.Query().Get("code")
	if code == "" {
		return "", fmt.Errorf("no code in redirect: %s", location)
	}
	return code, nil
}

func (c *Client) refreshAccessToken() error {
	if c.settings == nil {
		if err := c.fetchSettings(); err != nil {
			return err
		}
	}
	if err := c.getSessionToken(); err != nil {
		return err
	}
	code, err := c.getAuthorizationCode()
	if err != nil {
		return err
	}

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", c.settings.SSOClientIDAuthZ)
	data.Set("redirect_uri", "https://portal.arubainstanton.com")
	data.Set("code", code)
	data.Set("code_verifier", c.pkceChallenge.Verifier)

	tokenURL := fmt.Sprintf("%s%s", c.settings.SSOBaseURL, c.settings.SSOEndpointTokens)
	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token exchange: status %d, body %s", resp.StatusCode, string(body))
	}

	var tok AuthToken
	if err := json.Unmarshal(body, &tok); err != nil {
		return err
	}
	c.token = &tok
	c.tokenObtainedAt = time.Now()
	log.Printf("[auth] access token obtained, expires in %ds", tok.ExpiresIn)
	return nil
}

func (c *Client) isTokenExpired() bool {
	if c.token == nil || c.token.AccessToken == "" {
		return true
	}
	expiresAt := c.tokenObtainedAt.Add(time.Duration(c.token.ExpiresIn) * time.Second)
	return time.Now().Add(5 * time.Minute).After(expiresAt)
}

func (c *Client) Token() (string, error) {
	if c.isTokenExpired() {
		if err := c.refreshAccessToken(); err != nil {
			return "", err
		}
	}
	return c.token.AccessToken, nil
}

func (c *Client) ForceRefresh() error {
	return c.refreshAccessToken()
}
