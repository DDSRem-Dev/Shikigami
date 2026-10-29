package agent

import "connectrpc.com/connect/v2"

func unavailableWorker() error {
	return rpcError(connect.CodeFailedPrecondition, "WORKER_NOT_CONFIGURED")
}
