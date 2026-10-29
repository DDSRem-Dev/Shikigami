package limits

import "time"

// Port metadata and watches have independent single-OS-user budgets. TCP
// connection admission remains the worker's configured MaxConnections limit.
const (
	PortMetadataCalls     = 32
	PortWatches           = 32
	PortFirstFrameTimeout = 5 * time.Second
)
