package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mili/moxie/internal/scanner"
)

// Detect explains engine detection for a path or a stored game without
// modifying the database. It answers "why did moxie classify this the way it
// did?" by surfacing the matched rule and confidence.
//
//	moxie detect [--json] <path|id|name>
func Detect(args []string) {
	fs := flag.NewFlagSet("detect", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output as JSON")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie detect [--json] <path|id|name>\n")
		os.Exit(1)
	}

	target := fs.Arg(0)
	path := target
	if st, err := os.Stat(target); err != nil || !st.IsDir() {
		// Not a directory on disk — resolve it as a stored game instead.
		database := OpenDB()
		game := ResolveGame(database, target)
		database.Close()
		if game == nil {
			fmt.Fprintf(os.Stderr, "No such directory or game: %s\n", target)
			os.Exit(1)
		}
		path = game.Path
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}

	g := scanner.ScanSingle(abs)
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(g); err != nil {
			fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
			os.Exit(1)
		}
		return
	}

	fmt.Printf("Path:       %s\n", g.Path)
	fmt.Printf("Title:      %s\n", g.Title)
	fmt.Printf("Engine:     %s\n", g.Engine)
	fmt.Printf("Confidence: %.2f\n", g.Confidence)
	fmt.Printf("Matched by: %s\n", g.MatchedBy)
	fmt.Printf("Version:    %s\n", g.Version)
	if g.ExePath != "" {
		fmt.Printf("Exe:        %s\n", g.ExePath)
	}
}
