// Package portplugin applies the public port-plugin configuration contract.
package portplugin

import schema "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"

type Config = schema.Config

var Parse = schema.Parse
var Loopback = schema.Loopback
