package licentivo

import "testing"

func TestLinuxProcessorDisplay(t *testing.T) {
	for _, test := range []struct{ name, raw, want string }{
		{"x86", "processor : 0\nmodel name : Intel Core i7\n", "Intel Core i7"},
		{"arm64", "processor : 0\nCPU implementer : 0x41\nCPU architecture : 8\nCPU part : 0xd0c\n", "ARM CPU (implementer 0x41, part 0xd0c)"},
		{"arm32", "Processor : ARMv7 Processor rev 5\n", "ARMv7 Processor rev 5"},
		{"unavailable", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := linuxDisplayCPU(test.raw); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}
