package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	browserruntime "github.com/DDSRem-Dev/Shikigami/plugins/browser/runtime"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "unlock-keyring" || os.Args[1] == "provision-keyring") {
		if len(os.Args) != 2 {
			fmt.Fprintln(os.Stderr, "keyring actions accept no arguments or paths")
			os.Exit(2)
		}
		if err := browserruntime.RunKeyringService(os.Args[1] == "provision-keyring"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	config := flag.String("config", "", "Private immutable systemd runtime launch record")
	version := flag.Bool("version", false, "Print build version")
	flag.Parse()
	if *version || (flag.NArg() == 1 && flag.Arg(0) == "version") {
		fmt.Println(buildinfo.String("shiki-browser-runtime"))
		return
	}
	if *config == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "--config is required; launch via the browser plugin systemd user service")
		os.Exit(2)
	}
	if err := browserruntime.Run(*config); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
