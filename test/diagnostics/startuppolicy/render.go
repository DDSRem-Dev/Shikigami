// Command render produces the actual installer's client unit for the isolated
// startup-policy acceptance. It does not implement service detection.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/DDSRem-Dev/Shikigami/internal/install"
)

func main() {
	config := flag.String("config", "", "absolute fixture configuration")
	prefix := flag.String("prefix", "", "absolute fixture installation prefix")
	flag.Parse()
	if *config == "" || *prefix == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "config and prefix are required")
		os.Exit(2)
	}
	data, err := install.RenderService(install.Plan{Role: "client", Home: "/home/shikitest", User: "shikitest", Config: *config, Path: "/usr/bin:/bin"}, *prefix)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(data); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
