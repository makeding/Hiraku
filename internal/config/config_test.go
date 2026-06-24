package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateChannel(t *testing.T) {
	valid := []string{"27", "BS15_0", "S3:001", "BS4K.TEST-1"}
	for _, ch := range valid {
		if err := ValidateChannel(ch); err != nil {
			t.Fatalf("expected channel %q to be valid: %v", ch, err)
		}
	}

	invalid := []string{"", "BS 15", "BS15;rm", "BS15/0"}
	for _, ch := range invalid {
		if err := ValidateChannel(ch); err == nil {
			t.Fatalf("expected channel %q to be invalid", ch)
		}
	}
}

func TestExpandPipeline(t *testing.T) {
	mode := Mode{
		Record: [][]string{
			{"recdvb4k", "--dev", "0", "--acas", "<channel>", "-", "-"},
			{"hantto4k", "--frontend-descrambled", "-", "-"},
		},
	}

	got := ExpandPipeline(mode, "BS4K_1")
	if got[0][4] != "BS4K_1" {
		t.Fatalf("channel was not expanded: %#v", got)
	}
	if got[1][0] != "hantto4k" {
		t.Fatalf("pipeline was changed unexpectedly: %#v", got)
	}
}

func TestLoadDefaultsAllowIPv4CidrRanges(t *testing.T) {
	cfg, err := loadConfigJSON(t, `{
		"secret": "secret",
		"modes": {
			"BSCS": {
				"record": [["recdvb", "<channel>"]]
			}
		}
	}`)
	if err != nil {
		t.Fatal(err)
	}

	if !cfg.AllowsRemoteAddr("192.168.1.10:12345") {
		t.Fatal("expected RFC1918 address to be allowed by default")
	}
	if cfg.AllowsRemoteAddr("8.8.8.8:12345") {
		t.Fatal("expected public IPv4 address to be rejected by default")
	}
}

func TestLoadRejectsInvalidAllowIPv4CidrRanges(t *testing.T) {
	invalidRanges := []string{"fc00::/7", "192.168.0.1", "not-a-cidr"}
	for _, cidr := range invalidRanges {
		_, err := loadConfigJSON(t, `{
			"secret": "secret",
			"allowIPv4CidrRanges": ["`+cidr+`"],
			"modes": {
				"BSCS": {
					"record": [["recdvb", "<channel>"]]
				}
			}
		}`)
		if err == nil {
			t.Fatalf("expected CIDR %q to be rejected", cidr)
		}
	}
}

func TestAllowsRemoteAddr(t *testing.T) {
	cfg := Config{
		AllowIPv4CidrRanges: []string{"10.0.0.0/8", "192.168.0.0/16"},
	}

	allowed := []string{"10.1.2.3:12345", "192.168.10.20", "[::ffff:192.168.1.2]:12345"}
	for _, addr := range allowed {
		if !cfg.AllowsRemoteAddr(addr) {
			t.Fatalf("expected %q to be allowed", addr)
		}
	}

	rejected := []string{"172.16.0.1:12345", "8.8.8.8:12345", "[fc00::1]:12345", "not-an-addr"}
	for _, addr := range rejected {
		if cfg.AllowsRemoteAddr(addr) {
			t.Fatalf("expected %q to be rejected", addr)
		}
	}
}

func loadConfigJSON(t *testing.T, body string) (Config, error) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}
