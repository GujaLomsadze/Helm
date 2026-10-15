package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/GujaLomsadze/helm/internal/cli"
)

func main() {
	if name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe"); name == "trk" {
		if cli.RenamedNotice(os.Args[1:], os.Stderr) {
			cli.AdoptNewName(os.Stderr)
		}
	}
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
