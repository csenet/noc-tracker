package tracker

import (
	"context"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/csenet/noc-tracker/iap"
	"github.com/csenet/noc-tracker/instanton"
)

// Source identifies which controller reported a client sighting.
type Source string

const (
	SourceInstantOn Source = "instanton"
	SourceIAP       Source = "iap"
)

type Sighting struct {
	MAC         string    `json:"mac"`
	IPAddress   string    `json:"ip_address,omitempty"`
	HostName    string    `json:"host_name,omitempty"`
	AccessPoint string    `json:"access_point,omitempty"`
	SSID        string    `json:"ssid,omitempty"`
	Band        string    `json:"band,omitempty"`
	SignalDbm   int       `json:"signal_dbm,omitempty"`
	Source      Source    `json:"source"`
	SiteName    string    `json:"site_name,omitempty"`
	SeenAt      time.Time `json:"seen_at"`
}

// InstantOnSource is the minimal capability the tracker needs from an Instant
// On client. Defined as an interface so it can be nil-out when credentials
// aren't configured.
type InstantOnSource interface {
	Sites() ([]instanton.Site, error)
	WirelessClients(siteID string) ([]instanton.WirelessClient, error)
}

// IAPSource is the minimal capability the tracker needs from an IAP client.
type IAPSource interface {
	WirelessClients() ([]iap.WirelessClient, error)
}

type Tracker struct {
	instantOn InstantOnSource
	iap       IAPSource
	interval  time.Duration

	mu        sync.RWMutex
	byMAC     map[string]Sighting
	byIP      map[string]Sighting
	lastError string
	lastPoll  time.Time
}

func New(instantOn InstantOnSource, iapClient IAPSource, interval time.Duration) *Tracker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Tracker{
		instantOn: instantOn,
		iap:       iapClient,
		interval:  interval,
		byMAC:     map[string]Sighting{},
		byIP:      map[string]Sighting{},
	}
}

func (t *Tracker) Start(ctx context.Context) {
	t.refresh()
	go func() {
		ticker := time.NewTicker(t.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t.refresh()
			}
		}
	}()
}

func (t *Tracker) refresh() {
	byMAC := map[string]Sighting{}
	byIP := map[string]Sighting{}
	var errs []string

	if t.instantOn != nil {
		sites, err := t.instantOn.Sites()
		if err != nil {
			errs = append(errs, "instanton sites: "+err.Error())
		} else {
			for _, site := range sites {
				clients, err := t.instantOn.WirelessClients(site.ID)
				if err != nil {
					errs = append(errs, "instanton clients ("+site.Name+"): "+err.Error())
					continue
				}
				for _, c := range clients {
					if !isInstantOnClientOnline(c) {
						continue
					}
					mac := NormalizeMAC(c.MacAddress)
					if mac == "" {
						continue
					}
					s := Sighting{
						MAC:         mac,
						IPAddress:   strings.TrimSpace(c.IPAddress),
						HostName:    firstNonEmpty(c.HostName, c.Name),
						AccessPoint: c.DeviceName,
						SSID:        c.WirelessNetworkName,
						Band:        c.WirelessBand,
						SignalDbm:   c.SignalInDbm,
						Source:      SourceInstantOn,
						SiteName:    site.Name,
						SeenAt:      time.Now(),
					}
					byMAC[mac] = s
					if s.IPAddress != "" {
						byIP[s.IPAddress] = s
					}
				}
			}
		}
	}

	if t.iap != nil {
		clients, err := t.iap.WirelessClients()
		if err != nil {
			errs = append(errs, "iap clients: "+err.Error())
		} else {
			for _, c := range clients {
				mac := NormalizeMAC(c.MacAddress)
				if mac == "" {
					continue
				}
				// Instant On takes priority when both sources see the same MAC.
				if _, taken := byMAC[mac]; taken {
					continue
				}
				s := Sighting{
					MAC:         mac,
					IPAddress:   strings.TrimSpace(c.IPAddress),
					HostName:    c.Name,
					AccessPoint: c.AccessPoint,
					SSID:        c.ESSID,
					Source:      SourceIAP,
					SeenAt:      time.Now(),
				}
				byMAC[mac] = s
				if s.IPAddress != "" {
					if _, taken := byIP[s.IPAddress]; !taken {
						byIP[s.IPAddress] = s
					}
				}
			}
		}
	}

	t.mu.Lock()
	t.byMAC = byMAC
	t.byIP = byIP
	t.lastPoll = time.Now()
	t.lastError = strings.Join(errs, "; ")
	t.mu.Unlock()

	if len(errs) > 0 {
		log.Printf("[tracker] refresh completed with errors: %s", strings.Join(errs, "; "))
	} else {
		log.Printf("[tracker] refresh ok, %d clients", len(byMAC))
	}
}

func (t *Tracker) SightingByMAC(mac string) (Sighting, bool) {
	norm := NormalizeMAC(mac)
	if norm == "" {
		return Sighting{}, false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	s, ok := t.byMAC[norm]
	return s, ok
}

func (t *Tracker) SightingByIP(ip string) (Sighting, bool) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return Sighting{}, false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	s, ok := t.byIP[ip]
	return s, ok
}

func (t *Tracker) All() []Sighting {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]Sighting, 0, len(t.byMAC))
	for _, s := range t.byMAC {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MAC < out[j].MAC })
	return out
}

// KnownAPs returns the set of AP names currently seen in any sighting,
// sorted. Used by the floorplan UI to render an editable AP list even when
// no positions have been saved yet.
func (t *Tracker) KnownAPs() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	seen := map[string]bool{}
	for _, s := range t.byMAC {
		if s.AccessPoint != "" {
			seen[s.AccessPoint] = true
		}
	}
	out := make([]string, 0, len(seen))
	for ap := range seen {
		out = append(out, ap)
	}
	sort.Strings(out)
	return out
}

func (t *Tracker) Status() (lastPoll time.Time, lastError string, count int) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.lastPoll, t.lastError, len(t.byMAC)
}

// isInstantOnClientOnline filters out clients that the Instant On
// clientSummary endpoint returns as historical/offline entries. The endpoint
// reports both currently-connected and recently-disconnected sessions; the
// tracker only cares about "where is this MAC right now", so anything not
// currently up is dropped.
//
// Verified empirically against a live cloud portal (982 rows: 233 "up", 749
// "down"). Offline clients keep their last DeviceName populated, so the AP
// field alone isn't a reliable signal — Status is.
func isInstantOnClientOnline(c instanton.WirelessClient) bool {
	return strings.EqualFold(strings.TrimSpace(c.Status), "up")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
