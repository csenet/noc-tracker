// iap-dump opens an SSH session to an Aruba IAP, runs `show clients`, and
// dumps the raw output to stdout. Useful for verifying that the parser's
// column assumptions match what the real firmware emits.
//
// Usage:
//
//	IAP_PASSWORD=... go run ./cmd/iap-dump -host 192.168.0.80
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/joho/godotenv"

	"github.com/csenet/noc-tracker/iap"
)

func main() {
	_ = godotenv.Load()

	host := flag.String("host", os.Getenv("IAP_HOST"), "IAP host")
	port := flag.String("port", envOr("IAP_PORT", "22"), "IAP SSH port")
	user := flag.String("user", envOr("IAP_USERNAME", "admin"), "IAP SSH user")
	pass := flag.String("pass", os.Getenv("IAP_PASSWORD"), "IAP SSH password (defaults to $IAP_PASSWORD)")
	parse := flag.Bool("parse", false, "additionally print the parsed client list")
	flag.Parse()

	if *host == "" {
		fmt.Fprintln(os.Stderr, "host is required (set -host or IAP_HOST)")
		os.Exit(2)
	}
	if *pass == "" {
		fmt.Fprintln(os.Stderr, "password is required (set -pass or IAP_PASSWORD)")
		os.Exit(2)
	}

	c := iap.NewClient(*host, *port, *user, *pass)

	if *parse {
		clients, err := c.WirelessClients()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("parsed %d clients:\n", len(clients))
		for _, cl := range clients {
			fmt.Printf("  %s  %s  %-25s  ap=%-20s ssid=%s\n",
				cl.MacAddress, cl.IPAddress, cl.Name, cl.AccessPoint, cl.ESSID)
		}
		return
	}

	out, err := iap.DumpShowClients(c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		// Still print whatever we got so the user can see partial output.
	}
	fmt.Print(out)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
