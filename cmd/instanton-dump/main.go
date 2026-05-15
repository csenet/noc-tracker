// instanton-dump hits the Aruba Instant On cloud portal API and dumps the
// raw clientSummary JSON for the first site (or a site selected by name).
// Useful for inspecting undocumented fields like `status` /
// `connectionDurationInSeconds` to decide what counts as "online".
//
// Usage:
//
//	go run ./cmd/instanton-dump
//	go run ./cmd/instanton-dump -site "event_wifi"
//	go run ./cmd/instanton-dump -summary  # group/count by Status & DeviceName presence
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/joho/godotenv"

	"github.com/csenet/noc-tracker/instanton"
)

func main() {
	_ = godotenv.Load()

	user := flag.String("user", os.Getenv("ARUBA_USERNAME"), "Instant On username")
	pass := flag.String("pass", os.Getenv("ARUBA_PASSWORD"), "Instant On password")
	siteName := flag.String("site", "", "site name (default: first site)")
	summary := flag.Bool("summary", false, "print Status/DeviceName histogram instead of raw JSON")
	flag.Parse()

	if *user == "" || *pass == "" {
		fmt.Fprintln(os.Stderr, "ARUBA_USERNAME and ARUBA_PASSWORD required")
		os.Exit(2)
	}

	c := instanton.NewClient(*user, *pass)
	sites, err := c.Sites()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sites: %v\n", err)
		os.Exit(1)
	}
	if len(sites) == 0 {
		fmt.Fprintln(os.Stderr, "no sites")
		os.Exit(1)
	}

	site := sites[0]
	if *siteName != "" {
		found := false
		for _, s := range sites {
			if s.Name == *siteName {
				site = s
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "site %q not found. available:\n", *siteName)
			for _, s := range sites {
				fmt.Fprintf(os.Stderr, "  - %s (%s)\n", s.Name, s.ID)
			}
			os.Exit(1)
		}
	}

	if !*summary {
		raw, err := c.DumpClientSummary(site.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dump: %v\n", err)
		}
		var pretty any
		if json.Unmarshal(raw, &pretty) == nil {
			out, _ := json.MarshalIndent(pretty, "", "  ")
			fmt.Println(string(out))
			return
		}
		os.Stdout.Write(raw)
		return
	}

	clients, err := c.WirelessClients(site.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clients: %v\n", err)
		os.Exit(1)
	}

	statusCount := map[string]int{}
	macSeen := map[string]int{}
	withDevice := 0
	withoutDevice := 0
	for _, cl := range clients {
		statusCount[cl.Status]++
		macSeen[cl.MacAddress]++
		if cl.DeviceName != "" {
			withDevice++
		} else {
			withoutDevice++
		}
	}

	fmt.Printf("site: %s (%s)\n", site.Name, site.ID)
	fmt.Printf("total rows: %d\n", len(clients))
	fmt.Printf("unique MACs: %d\n", len(macSeen))
	fmt.Printf("with deviceName: %d\n", withDevice)
	fmt.Printf("without deviceName: %d\n", withoutDevice)

	fmt.Println("\nstatus histogram:")
	statuses := make([]string, 0, len(statusCount))
	for k := range statusCount {
		statuses = append(statuses, k)
	}
	sort.Slice(statuses, func(i, j int) bool { return statusCount[statuses[i]] > statusCount[statuses[j]] })
	for _, s := range statuses {
		label := s
		if label == "" {
			label = "(empty)"
		}
		fmt.Printf("  %-20s %d\n", label, statusCount[s])
	}

	dupCount := map[int]int{}
	for _, n := range macSeen {
		dupCount[n]++
	}
	fmt.Println("\nrows-per-MAC histogram:")
	counts := make([]int, 0, len(dupCount))
	for k := range dupCount {
		counts = append(counts, k)
	}
	sort.Ints(counts)
	for _, k := range counts {
		fmt.Printf("  %d rows/MAC: %d MACs\n", k, dupCount[k])
	}
}
