// This file defines the custom error types returned by the Vectron client.
// These errors provide a convenient way for client applications to check for
// specific failure modes using `errors.Is`.

package vectron

import "errors"

var (
	// ErrNotFound is returned when a requested resource (e.g., a collection or point) does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInvalidArgument is returned when a request contains invalid arguments, such as an empty collection name.
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrAlreadyExists is returned when attempting to create a resource that already exists.
	ErrAlreadyExists = errors.New("already exists")
	// ErrAuthentication is returned when the API key is invalid or missing.
	ErrAuthentication = errors.New("authentication failed")
	// ErrInternalServer is returned for unhandled errors on the server side.
	ErrInternalServer = errors.New("internal server error")
)
