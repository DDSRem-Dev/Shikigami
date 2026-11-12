package client

import (
	"github.com/DDSRem-Dev/Shikigami/internal/pluginmarket"
	"github.com/DDSRem-Dev/Shikigami/internal/strictjson"
)

// Plugin marketplace verification is exposed through the frontend client
// package so CLI code does not bypass the client boundary.
type PluginMarketTrustStore = pluginmarket.TrustStore
type PluginMarketTrustRoot = pluginmarket.TrustRoot
type PluginMarketIndex = pluginmarket.Index
type PluginMarketEntry = pluginmarket.Entry

const (
	PluginMarketMaxURLLength             = pluginmarket.MaxURLLength
	PluginMarketMaxEnvelopeBytes         = pluginmarket.MaxEnvelopeBytes
	PluginMarketMaxArchiveSignatureBytes = pluginmarket.MaxArchiveSignatureBytes
)

func NewPluginMarketTrustStore() PluginMarketTrustStore {
	return pluginmarket.NewTrustStore()
}

func AddPluginMarketTrustRoot(store *PluginMarketTrustStore, root PluginMarketTrustRoot) error {
	return store.AddRoot(root)
}

func LoadPluginMarketTrustStore(path string) (PluginMarketTrustStore, error) {
	return pluginmarket.LoadTrustStore(path)
}

func SavePluginMarketTrustStore(path string, store PluginMarketTrustStore) error {
	return pluginmarket.SaveTrustStore(path, store)
}

func ValidatePluginMarketID(id string) error {
	return pluginmarket.ValidatePluginID(id)
}

func VerifyPluginMarketIndex(data []byte, store PluginMarketTrustStore) (PluginMarketIndex, error) {
	return pluginmarket.VerifyIndex(data, store)
}

func VerifyPluginMarketArchive(path string, signature []byte, entry PluginMarketEntry, store PluginMarketTrustStore) error {
	return pluginmarket.VerifyArchive(path, signature, entry, store)
}

func DecodeStrictJSON(data []byte, out any) error {
	return strictjson.Decode(data, out)
}
