// Package limits defines message bounds shared by SDK adapters and workers.
package limits

const (
	MaxMessageBytes = 1 << 20
	MaxDataBytes    = 256 << 10
)
