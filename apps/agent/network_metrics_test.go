package main

import (
	"strings"
	"testing"
)

func TestHostCounterParserBoundedStableAndNoFabricatedTraffic(t *testing.T) {
	row := func(name, in, out string) string {
		return name + ": " + in + " 0 0 0 0 0 0 0 " + out + " 0 0 0 0 0 0 0\n"
	}
	a := row("eth0", "100", "200")
	b := row("tun0", "30", "40")
	rx, tx, d, e := parseProcNetDev([]byte("Inter-| Receive\n" + a + b + row("lo", "999", "999")))
	if e != nil || rx != 130 || tx != 240 || len(d) != 64 {
		t.Fatalf("counters %d %d %s %v", rx, tx, d, e)
	}
	_, _, other, e := parseProcNetDev([]byte(b + a))
	if e != nil || other != d {
		t.Fatal("interface order changed identity")
	}
	for _, raw := range []string{"", row("lo", "5", "6"), a + a, row("eth0", "-1", "0"), row("eth0", "9007199254740992", "0"), "eth0: 1 2", strings.Repeat("x", 1<<20+1)} {
		if _, _, _, e := parseProcNetDev([]byte(raw)); e == nil {
			t.Fatal("malformed/unavailable counters accepted")
		}
	}
}
