package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"regexp"
	"strings"
)

const DefaultMaxConcurrentPipes = 4

type Config struct {
	Listen                      string          `json:"listen"`
	Secret                      string          `json:"secret"`
	AllowIPv4CidrRanges         []string        `json:"allowIPv4CidrRanges"`
	DisconnectCloseDelaySeconds int             `json:"disconnectCloseDelaySeconds"`
	MaxConcurrentPipes          int             `json:"maxConcurrentPipes"`
	Modes                       map[string]Mode `json:"modes"`
	Pipes                       map[string]Pipe `json:"pipes"`
}

type Mode struct {
	Record [][]string `json:"record"`
}

type Pipe struct {
	Command [][]string `json:"command"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:40773"
	}
	if cfg.AllowIPv4CidrRanges == nil {
		cfg.AllowIPv4CidrRanges = defaultAllowIPv4CidrRanges()
	}
	if cfg.Secret == "" {
		return Config{}, errors.New("secret is required")
	}
	if cfg.DisconnectCloseDelaySeconds < 0 {
		return Config{}, errors.New("disconnectCloseDelaySeconds must be greater than or equal to 0")
	}
	if cfg.MaxConcurrentPipes < 0 {
		return Config{}, errors.New("maxConcurrentPipes must be greater than or equal to 0")
	}
	if cfg.MaxConcurrentPipes == 0 {
		cfg.MaxConcurrentPipes = DefaultMaxConcurrentPipes
	}
	for _, cidr := range cfg.AllowIPv4CidrRanges {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil || !prefix.Addr().Is4() {
			return Config{}, fmt.Errorf("invalid allowIPv4CidrRanges entry: %q", cidr)
		}
	}
	if len(cfg.Modes) == 0 && len(cfg.Pipes) == 0 {
		return Config{}, errors.New("at least one mode or pipe is required")
	}
	for name, pipe := range cfg.Pipes {
		if err := ValidatePipeName(name); err != nil {
			return Config{}, err
		}
		if err := validatePipeline("pipe", name, pipe.Command); err != nil {
			return Config{}, err
		}
	}
	for name, mode := range cfg.Modes {
		if err := ValidateModeName(name); err != nil {
			return Config{}, err
		}
		if len(mode.Record) == 0 {
			return Config{}, fmt.Errorf("mode %q has no record pipeline", name)
		}
		for i, argv := range mode.Record {
			if len(argv) == 0 {
				return Config{}, fmt.Errorf("mode %q pipeline step %d is empty", name, i)
			}
			for _, arg := range argv {
				if strings.Contains(arg, "\x00") {
					return Config{}, fmt.Errorf("mode %q contains NUL byte", name)
				}
			}
		}
	}

	return cfg, nil
}

func defaultAllowIPv4CidrRanges() []string {
	return []string{"10.0.0.0/8", "127.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
}

func (cfg Config) AllowsRemoteAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}

	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	if !addr.Is4() {
		return false
	}

	for _, cidr := range cfg.AllowIPv4CidrRanges {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			continue
		}
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

var modeNamePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]*$`)

func ValidateModeName(mode string) error {
	if !modeNamePattern.MatchString(mode) {
		return fmt.Errorf("invalid mode: %q", mode)
	}
	return nil
}

func ValidatePipeName(name string) error {
	if !modeNamePattern.MatchString(name) {
		return fmt.Errorf("invalid pipe: %q", name)
	}
	return nil
}

func validatePipeline(kind string, name string, pipeline [][]string) error {
	if len(pipeline) == 0 {
		return fmt.Errorf("%s %q has no command pipeline", kind, name)
	}
	for i, argv := range pipeline {
		if len(argv) == 0 {
			return fmt.Errorf("%s %q pipeline step %d is empty", kind, name, i)
		}
		for _, arg := range argv {
			if strings.Contains(arg, "\x00") {
				return fmt.Errorf("%s %q contains NUL byte", kind, name)
			}
		}
	}
	return nil
}

var channelPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)

func ValidateChannel(channel string) error {
	if !channelPattern.MatchString(channel) {
		return fmt.Errorf("invalid channel: %q", channel)
	}
	return nil
}

func ExpandPipeline(mode Mode, channel string) [][]string {
	pipeline := make([][]string, 0, len(mode.Record))
	for _, argv := range mode.Record {
		step := make([]string, 0, len(argv))
		for _, arg := range argv {
			step = append(step, strings.ReplaceAll(arg, "<channel>", channel))
		}
		pipeline = append(pipeline, step)
	}
	return pipeline
}
