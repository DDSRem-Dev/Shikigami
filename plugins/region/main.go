package main

import (
	"flag"
	"fmt"
	"os"

	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	"github.com/DDSRem-Dev/Shikigami/plugins/region/server"
)

func main() {
	side := flag.String("side", "", "Plugin side: server")
	version := flag.Bool("version", false, "Print build version")
	flag.Parse()
	if *version || (flag.NArg() == 1 && flag.Arg(0) == "version") {
		fmt.Println(buildinfo.String("shiki-plugin-region"))
		return
	}
	if *side != "server" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "region plugin requires --side=server")
		os.Exit(1)
	}
	worker := server.New()
	if err := sdk.Serve("server", worker, sdk.Services{Region: worker}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
