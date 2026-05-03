package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/mili/moxie/internal/util"
)

// Config handles the config command and its subcommands.
func Config(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie config <set|get|show> [key] [value]\n")
		fmt.Fprintf(os.Stderr, "       moxie config set steamgriddb-key <key>\n")
		fmt.Fprintf(os.Stderr, "       moxie config get steamgriddb-key\n")
		fmt.Fprintf(os.Stderr, "       moxie config show\n")
		os.Exit(1)
	}
	switch args[0] {
	case "set":
		ConfigSet(args[1:])
	case "get":
		ConfigGet(args[1:])
	case "show":
		ConfigShow()
	default:
		fmt.Fprintf(os.Stderr, "Unknown config subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

// ConfigSet sets a configuration value.
func ConfigSet(args []string) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: moxie config set <key> <value>\n")
		os.Exit(1)
	}
	key := args[0]
	value := strings.Join(args[1:], " ")

	cfg, err := util.ReadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
		os.Exit(1)
	}
	cfg[key] = value
	if err := util.WriteConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Set %s = %s\n", key, value)
}

// ConfigGet gets a configuration value.
func ConfigGet(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie config get <key>\n")
		os.Exit(1)
	}
	cfg, err := util.ReadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
		os.Exit(1)
	}
	value, ok := cfg[args[0]]
	if !ok || value == "" {
		fmt.Fprintf(os.Stderr, "(not set)\n")
		return
	}
	fmt.Println(value)
}

// ConfigShow shows all configuration values.
func ConfigShow() {
	cfg, err := util.ReadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
		os.Exit(1)
	}
	if len(cfg) == 0 {
		fmt.Println("No configuration values set.")
		return
	}
	for k, v := range cfg {
		masked := v
		if strings.Contains(strings.ToLower(k), "key") && len(v) > 4 {
			masked = v[:4] + strings.Repeat("*", len(v)-4)
		}
		fmt.Printf("  %-25s %s\n", k, masked)
	}
}
