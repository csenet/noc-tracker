package iap

import "testing"

// realSample is a verbatim slice of the noc-ap01 device output (ArubaOS
// Instant 8.x), trimmed to ~10 rows that cover the edge cases observed in
// the full 478-client capture: blank Name column, blank OS, AP name with
// parens, "--" IPv6, eduroam-style long usernames, and the trailer line.
//
// The leading shell banner and `noc-ap01# show clients` echo line are
// included so the parser exercises its header-anchoring logic.
const realSample = `クラウドネイティブ会議NOC

show tech-support and show tech-support supplemental are the two most useful outputs to collect for any kind of troubleshooting session.

noc-ap01# show clients


Client List
-----------
Name                              IP Address       MAC Address        OS       ESSID                   Access Point      Channel  Type  Role      IPv6 Address                        Signal    Speed (mbps)
----                              ----------       -----------        --       -----                   ------------      -------  ----  ----      ------------                        ------    ------------
Pixel-10-Pro-Fold                 10.32.24.143     ca:2b:33:fc:1b:5c  Linux    CloudNative-Kaigi       foyer-ap03        64-      a-HE  user      2001:3b0:2a:78:4001:f545:c96a:239   37(good)  344(good)
Mac                               10.32.23.155     92:1b:a4:ac:87:f3  NOFP     CloudNative-Kaigi       foyer-ap03        64-      a-HE  user      2001:3b0:2a:78:18ab:af4a:baa1:b149  10(poor)  541(good)
                                  10.32.21.152     a2:c3:cc:a5:db:7c  NOFP     CloudNative-Kaigi       yobi-ap03(foyer)  108+     a-HE  user      2001:3b0:2a:78:e06e:6cd3:af79:e408  11(poor)  433(good)
kaede-laptop02                    192.168.1.9      d0:39:57:3d:93:6f  Win 11   CloudNative-Kaigi-Mgmt  noc-ap01          36+      AC    mgmt      fe80::9153:7246:75a5:a8bf           42(good)  400(good)
2312110189v@eduroam.kindai.ac.jp  10.32.23.210     ee:10:8d:9a:46:30  NOFP     eduroam                 noc-ap01          36+      AC    eduroam   2001:3b0:2a:78:f8b9:c075:f253:db1d  42(good)  360(good)
iPad                              10.32.24.43      46:3c:ee:fa:ee:41           CloudNative-Kaigi       hall-ap02         124+     a-HE  user      2001:3b0:2a:78:6ded:629a:b68f:69a7  43(good)  458(good)
EXTL248                           10.32.21.221     8c:e9:ee:0f:86:dd  Win 11   CloudNative-Kaigi       hall-ap01         56-      a-HE  user      --                                  41(good)  275(good)
                                  0.0.0.0          fe:2f:51:ff:62:8d  NOFP     eduroam                 yobi-ap01(foyer)  100+     a-HE  N/A       --                                  51(good)  6(poor)
Number of Clients   :478
Info timestamp      :92335
noc-ap01# exit
`

func TestParseShowClients_RealOutput(t *testing.T) {
	clients := parseShowClients(realSample)
	if got, want := len(clients), 8; got != want {
		t.Fatalf("parsed %d clients, want %d", got, want)
	}

	// Spot-check by MAC because the order is deterministic.
	byMAC := map[string]WirelessClient{}
	for _, c := range clients {
		byMAC[c.MacAddress] = c
	}

	check := func(mac string, expect WirelessClient) {
		t.Helper()
		got, ok := byMAC[mac]
		if !ok {
			t.Errorf("MAC %s missing from parse", mac)
			return
		}
		if got.Name != expect.Name {
			t.Errorf("%s: Name=%q want %q", mac, got.Name, expect.Name)
		}
		if got.IPAddress != expect.IPAddress {
			t.Errorf("%s: IP=%q want %q", mac, got.IPAddress, expect.IPAddress)
		}
		if got.AccessPoint != expect.AccessPoint {
			t.Errorf("%s: AP=%q want %q", mac, got.AccessPoint, expect.AccessPoint)
		}
		if got.ESSID != expect.ESSID {
			t.Errorf("%s: ESSID=%q want %q", mac, got.ESSID, expect.ESSID)
		}
		if got.Signal != expect.Signal {
			t.Errorf("%s: Signal=%q want %q", mac, got.Signal, expect.Signal)
		}
	}

	// Normal row.
	check("ca:2b:33:fc:1b:5c", WirelessClient{
		Name: "Pixel-10-Pro-Fold", IPAddress: "10.32.24.143",
		AccessPoint: "foyer-ap03", ESSID: "CloudNative-Kaigi", Signal: "37(good)",
	})

	// Blank Name column.
	check("a2:c3:cc:a5:db:7c", WirelessClient{
		Name: "", IPAddress: "10.32.21.152",
		AccessPoint: "yobi-ap03(foyer)", ESSID: "CloudNative-Kaigi", Signal: "11(poor)",
	})

	// Long username with `@`.
	check("ee:10:8d:9a:46:30", WirelessClient{
		Name: "2312110189v@eduroam.kindai.ac.jp", IPAddress: "10.32.23.210",
		AccessPoint: "noc-ap01", ESSID: "eduroam", Signal: "42(good)",
	})

	// Blank OS column — must not throw off the IPv6/Signal/Speed alignment.
	check("46:3c:ee:fa:ee:41", WirelessClient{
		Name: "iPad", IPAddress: "10.32.24.43",
		AccessPoint: "hall-ap02", ESSID: "CloudNative-Kaigi", Signal: "43(good)",
	})

	// IPv6 column shown as "--", AP name with parens, blank Name.
	check("fe:2f:51:ff:62:8d", WirelessClient{
		Name: "", IPAddress: "0.0.0.0",
		AccessPoint: "yobi-ap01(foyer)", ESSID: "eduroam", Signal: "51(good)",
	})
}

func TestHasClientHeader(t *testing.T) {
	if !hasClientHeader("Name  IP Address  MAC Address  OS") {
		t.Error("hasClientHeader should match standard header")
	}
	if hasClientHeader("noc-ap01# ") {
		t.Error("hasClientHeader should not match the prompt")
	}
}
