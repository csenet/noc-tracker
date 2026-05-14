package iap

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Client drives an Aruba IAP through its interactive SSH CLI. The web UI
// (swarm.cgi) is also available but adds session/sid juggling and an XML
// envelope; the SSH path is simpler and proven against the real device.
//
// Aruba IAP rejects exec channels with "Only cli connections are allowed to
// the AP", so we have to attach a PTY and wait for the `#` prompt between
// commands. We delegate that to expect(1) rather than reimplement
// prompt-driven I/O in Go.
type Client struct {
	host     string
	port     string
	username string
	password string
}

func NewClient(host, port, username, password string) *Client {
	if port == "" {
		port = "22"
	}
	return &Client{host: host, port: port, username: username, password: password}
}

type WirelessClient struct {
	Name        string
	IPAddress   string
	MacAddress  string
	ESSID       string
	AccessPoint string
	Channel     string
	Signal      string
}

func (c *Client) WirelessClients() ([]WirelessClient, error) {
	output, err := c.runShowClients()
	if err != nil {
		return nil, err
	}
	return parseShowClients(output), nil
}

// DumpShowClients runs `show clients` and returns the raw text. Exposed for
// the iap-dump command so callers can verify the parser against unparsed
// firmware output when something looks off.
func DumpShowClients(c *Client) (string, error) {
	return c.runShowClients()
}

// expectScript logs in, runs `show clients`, and exits. We deliberately do
// NOT send `no paging` — the IAP firmware tested (ArubaOS Instant 8.x)
// returns `% Parse error` for it; the command also turns out to be
// unnecessary because `show clients` streams the whole table without a pager
// over `ssh -tt`. Credentials are passed via env vars to keep them out of
// the process argument list.
const expectScript = `
log_user 1
set timeout 20

set host $env(NOC_IAP_HOST)
set port $env(NOC_IAP_PORT)
set user $env(NOC_IAP_USER)
set pass $env(NOC_IAP_PASS)

spawn ssh -tt \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -o LogLevel=ERROR \
    -o ConnectTimeout=5 \
    -o PreferredAuthentications=password,keyboard-interactive \
    -o PubkeyAuthentication=no \
    -p $port $user@$host

expect {
    -re "(?i)password:" { send -- "$pass\r" }
    timeout { puts stderr "TIMEOUT_PASSWORD"; exit 2 }
    eof     { puts stderr "EOF_BEFORE_PASSWORD"; exit 3 }
}

expect {
    "#" {}
    timeout { puts stderr "TIMEOUT_PROMPT_AFTER_LOGIN"; exit 4 }
    eof     { puts stderr "EOF_AFTER_LOGIN"; exit 5 }
}

send -- "show clients\r"
# 478 clients took ~5s on the test device; give big sites room to grow.
set timeout 60
expect {
    -re "Number of Clients\\s*:" {}
    timeout { puts stderr "TIMEOUT_SHOW_CLIENTS"; exit 6 }
    eof     { puts stderr "EOF_DURING_SHOW_CLIENTS"; exit 7 }
}
expect "#"

send -- "exit\r"
expect eof
`

func (c *Client) runShowClients() (string, error) {
	if _, err := exec.LookPath("expect"); err != nil {
		return "", fmt.Errorf("expect(1) not found in PATH: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "expect", "-c", expectScript)
	cmd.Env = append(cmd.Environ(),
		"NOC_IAP_HOST="+c.host,
		"NOC_IAP_PORT="+c.port,
		"NOC_IAP_USER="+c.username,
		"NOC_IAP_PASS="+c.password,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := stdout.String()
	if err != nil && !hasClientHeader(out) {
		return out, fmt.Errorf("expect/ssh failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

var macRE = regexp.MustCompile(`(?i)\b([0-9a-f]{2}[:\-]){5}[0-9a-f]{2}\b`)

func hasClientHeader(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "mac address") && strings.Contains(l, "ip address")
}

// parseShowClients parses Aruba IAP `show clients` output. The CLI prints a
// fixed-width table with a header followed by a dashed underline, then rows.
// We anchor on the header line ("MAC Address" is always present), compute
// column start positions from the dash-underline row, and slice each data row
// by those positions. Whitespace splitting fails because Name, ESSID, and AP
// name can contain spaces, and Name is frequently blank.
func parseShowClients(output string) []WirelessClient {
	// ssh -tt sprinkles CR into the output; strip them so column math works.
	output = strings.ReplaceAll(output, "\r", "")
	lines := strings.Split(output, "\n")

	headerIdx := -1
	for i, line := range lines {
		l := strings.ToLower(line)
		if strings.Contains(l, "mac address") && strings.Contains(l, "ip address") {
			headerIdx = i
			break
		}
	}
	if headerIdx < 0 || headerIdx+1 >= len(lines) {
		return nil
	}

	header := lines[headerIdx]
	dashes := lines[headerIdx+1]
	if !strings.Contains(dashes, "---") {
		return nil
	}

	cols := columnBounds(dashes)
	headerNames := map[string]int{}
	for i, c := range cols {
		end := len(header)
		if i+1 < len(cols) {
			end = cols[i+1].start
		}
		if c.start >= len(header) {
			continue
		}
		if end > len(header) {
			end = len(header)
		}
		name := strings.TrimSpace(strings.ToLower(header[c.start:end]))
		headerNames[name] = i
	}

	idx := func(names ...string) int {
		for _, n := range names {
			if v, ok := headerNames[n]; ok {
				return v
			}
		}
		return -1
	}
	nameI := idx("name", "client", "user name")
	ipI := idx("ip address")
	macI := idx("mac address")
	essidI := idx("essid", "network", "ssid")
	apI := idx("access point", "ap name", "ap")
	chI := idx("channel")
	sigI := idx("signal")

	var clients []WirelessClient
	for i := headerIdx + 2; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(strings.ToLower(line), "number of clients") {
			break
		}
		if !macRE.MatchString(line) {
			continue
		}
		fields := sliceColumns(line, cols)
		wc := WirelessClient{}
		if nameI >= 0 && nameI < len(fields) {
			wc.Name = fields[nameI]
		}
		if ipI >= 0 && ipI < len(fields) {
			wc.IPAddress = fields[ipI]
		}
		if macI >= 0 && macI < len(fields) {
			wc.MacAddress = fields[macI]
		} else {
			wc.MacAddress = macRE.FindString(line)
		}
		if essidI >= 0 && essidI < len(fields) {
			wc.ESSID = fields[essidI]
		}
		if apI >= 0 && apI < len(fields) {
			wc.AccessPoint = fields[apI]
		}
		if chI >= 0 && chI < len(fields) {
			wc.Channel = fields[chI]
		}
		if sigI >= 0 && sigI < len(fields) {
			wc.Signal = fields[sigI]
		}
		if wc.MacAddress != "" {
			clients = append(clients, wc)
		}
	}
	return clients
}

type colBound struct{ start, length int }

func columnBounds(dashes string) []colBound {
	var out []colBound
	i := 0
	for i < len(dashes) {
		if dashes[i] == '-' {
			start := i
			for i < len(dashes) && dashes[i] == '-' {
				i++
			}
			out = append(out, colBound{start: start, length: i - start})
		} else {
			i++
		}
	}
	return out
}

func sliceColumns(line string, cols []colBound) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		if c.start >= len(line) {
			out[i] = ""
			continue
		}
		end := c.start + c.length
		if i+1 < len(cols) {
			end = cols[i+1].start
		}
		if end > len(line) {
			end = len(line)
		}
		out[i] = strings.TrimSpace(line[c.start:end])
	}
	return out
}
