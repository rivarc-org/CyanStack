package interfaces

import "errors"

// ErrContainerNotFound is returned when a container lookup by name fails
var ErrContainerNotFound = errors.New("container not found")
