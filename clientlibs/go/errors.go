package vectron

import "errors"

var (
	ErrNotFound        = errors.New("not found")
	ErrInvalidArgument = errors.New("invalid argument")
	ErrAlreadyExists   = errors.New("already exists")
	ErrAuthentication  = errors.New("authentication failed")
	ErrInternalServer  = errors.New("internal server error")
)
