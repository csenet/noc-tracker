package instanton

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/csenet/noc-tracker/auth"
)

type Client struct {
	auth       *auth.Client
	httpClient *http.Client
	baseURL    string
	apiVersion string
}

func NewClient(username, password string) *Client {
	return &Client{
		auth:       auth.NewClient(username, password),
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    "https://portal.instant-on.hpe.com/api",
		apiVersion: "24",
	}
}

func (c *Client) request(method, endpoint string) (*http.Response, error) {
	token, err := c.auth.Token()
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}

	req, err := http.NewRequest(method, c.baseURL+endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-ion-api-version", c.apiVersion)
	req.Header.Set("x-ion-client-platform", "web")
	req.Header.Set("x-ion-client-type", "InstantOn")

	resp, err := c.httpClient.Do(req)
	if err == nil && resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		if err := c.auth.ForceRefresh(); err != nil {
			return nil, fmt.Errorf("refresh after 401: %w", err)
		}
		token, err = c.auth.Token()
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err = c.httpClient.Do(req)
	}
	return resp, err
}

type Site struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Health string `json:"health"`
	Status string `json:"status"`
}

type sitesResponse struct {
	Elements []Site `json:"elements"`
}

func (c *Client) Sites() ([]Site, error) {
	resp, err := c.request("GET", "/sites/")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sites: status %d, body %s", resp.StatusCode, string(body))
	}
	var sr sitesResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, err
	}
	return sr.Elements, nil
}

type WirelessClient struct {
	ID                          string `json:"id"`
	Name                        string `json:"name"`
	HostName                    string `json:"hostName"`
	WirelessNetworkName         string `json:"wirelessNetworkName"`
	IPAddress                   string `json:"ipAddress"`
	MacAddress                  string `json:"macAddress"`
	DeviceName                  string `json:"deviceName"`
	DeviceId                    string `json:"deviceId"`
	WirelessBand                string `json:"wirelessBand"`
	SignalQuality               string `json:"signalQuality"`
	SignalInDbm                 int    `json:"signalInDbm"`
	Status                      string `json:"status"`
	ConnectionDurationInSeconds int    `json:"connectionDurationInSeconds"`
}

type clientSummaryResponse struct {
	Elements []WirelessClient `json:"elements"`
}

func (c *Client) WirelessClients(siteID string) ([]WirelessClient, error) {
	resp, err := c.request("GET", "/sites/"+siteID+"/clientSummary")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clientSummary: status %d, body %s", resp.StatusCode, string(body))
	}
	var cr clientSummaryResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, err
	}
	return cr.Elements, nil
}

// DumpClientSummary returns the raw clientSummary JSON for a site. Exposed for
// the instanton-dump command so callers can inspect undocumented fields
// (status, connectionDurationInSeconds, etc.) without a typed struct in the
// way.
func (c *Client) DumpClientSummary(siteID string) ([]byte, error) {
	resp, err := c.request("GET", "/sites/"+siteID+"/clientSummary")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return body, fmt.Errorf("clientSummary: status %d", resp.StatusCode)
	}
	return body, nil
}
