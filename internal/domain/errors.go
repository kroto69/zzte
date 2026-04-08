package domain

import "errors"

// Common errors
var (
	ErrOLTNotFound      = errors.New("OLT not found")
	ErrOLTAlreadyExists = errors.New("OLT already exists")
	ErrONUNotFound      = errors.New("ONU not found")
	ErrSNMPTimeout      = errors.New("SNMP timeout")
	ErrSNMPConnection   = errors.New("SNMP connection failed")
	ErrInvalidParam     = errors.New("invalid parameter")
	ErrCacheError       = errors.New("cache error")
)
