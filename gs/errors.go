package gs

import "errors"

var (
	ErrNotRegistered = errors.New("gs: type not registered")
	ErrNoSuchField   = errors.New("gs: no such field")
	ErrNoSuchMethod  = errors.New("gs: no such method")
	ErrReadOnly      = errors.New("gs: field is readonly")
	ErrNotSettable   = errors.New("gs: field is not settable")
	ErrNeedPointer   = errors.New("gs: instance must be a pointer to be modified (law 3: settability)")
	ErrBadArg        = errors.New("gs: argument type mismatch")
)
