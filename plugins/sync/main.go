package main

import (
	"flag"
	"fmt"
	"os"

	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	"github.com/DDSRem-Dev/Shikigami/plugins/sync/client"
	"github.com/DDSRem-Dev/Shikigami/plugins/sync/server"
)

func main() {
	side := flag.String("side", "", "Plugin side: client or server")
	version := flag.Bool("version", false, "Print build version")
	flag.Parse()
	if *version {
		fmt.Println(buildinfo.String("shiki-plugin-sync"))
		return
	}
	var err error
	switch *side {
	case "client":
		worker := client.New()
		err = sdk.Serve(*side, worker, sdk.Services{Sync: worker})
	case "server":
		worker := server.New()
		err = sdk.Serve(*side, worker, sdk.Services{Files: worker})
	default:
		err = fmt.Errorf("--side must be client or server")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
