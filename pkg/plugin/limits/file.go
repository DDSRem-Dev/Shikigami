package limits

import "time"

// File admission is independent of terminal and port traffic. These budgets
// apply to the single OS-user service domain, not individual connections.
const (
	FileMetadataCalls     = 32
	FileTransfers         = 16
	FileWatches           = 32
	FileFirstFrameTimeout = 5 * time.Second
)
