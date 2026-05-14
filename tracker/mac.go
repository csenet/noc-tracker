package tracker

import "strings"

// NormalizeMAC canonicalizes a MAC into lower-case hex digits separated by
// colons (aa:bb:cc:dd:ee:ff). Accepts any of the common formats Aruba devices
// emit: colon, dash, or dotted-Cisco (aabb.ccdd.eeff). Returns "" if the input
// doesn't decode to exactly 12 hex characters.
func NormalizeMAC(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(12)
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			b.WriteRune(r)
		}
	}
	hex := b.String()
	if len(hex) != 12 {
		return ""
	}
	out := make([]byte, 0, 17)
	for i := 0; i < 12; i += 2 {
		if i > 0 {
			out = append(out, ':')
		}
		out = append(out, hex[i], hex[i+1])
	}
	return string(out)
}
