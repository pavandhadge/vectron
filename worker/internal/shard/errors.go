package shard

import "errors"

var (
	ErrShardNotReady            = errors.New("shard not ready")
	ErrLinearizableNotSupported = errors.New("linearizable reads not supported")
)
