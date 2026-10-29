// Package storage owns daemon resource validation. Atomic state persistence is
// shared with subprocess workers through the plugin SDK's state package.
package storage

import state "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"

const SchemaVersion = state.SchemaVersion

type Store = state.Store
type Snapshot = state.Snapshot
type RequestRecord = state.RequestRecord
type Options = state.Options

var (
	ErrLocked           = state.ErrLocked
	ErrCorrupt          = state.ErrCorrupt
	ErrFutureSchema     = state.ErrFutureSchema
	ErrMigrationBackup  = state.ErrMigrationBackup
	ErrStateTooLarge    = state.ErrStateTooLarge
	ErrRequestMismatch  = state.ErrRequestMismatch
	ErrClosed           = state.ErrClosed
	ErrDirectoryChanged = state.ErrDirectoryChanged
	Open                = state.Open
	EnsurePrivateDir    = state.EnsurePrivateDir
	ReadPrivateFile     = state.ReadPrivateFile
	AtomicWrite         = state.AtomicWrite
)
