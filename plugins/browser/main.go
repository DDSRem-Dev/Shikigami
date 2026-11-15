package main

import (
	"flag"
	"fmt"
	"os"

	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	"github.com/DDSRem-Dev/Shikigami/plugins/browser/client"
	"github.com/DDSRem-Dev/Shikigami/plugins/browser/server"
)

func main() {
	side := flag.String("side", "", "Plugin side: client or server")
	version := flag.Bool("version", false, "Print build version")
	flag.Parse()
	if *version || (flag.NArg() == 1 && flag.Arg(0) == "version") {
		fmt.Println(buildinfo.String("shiki-plugin-browser"))
		return
	}
	var err error
	switch *side {
	case "client":
		worker := client.New()
		err = sdk.Serve(*side, worker, sdk.Services{BrowserLocal: worker})
	case "server":
		worker := server.New()
		err = sdk.Serve(*side, worker, sdk.Services{Browser: worker})
	default:
		err = fmt.Errorf("--side must be client or server")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
