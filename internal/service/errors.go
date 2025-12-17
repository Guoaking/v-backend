package service

import "errors"

var (
    ErrUpstreamUnavailable = errors.New("UPSTREAM_UNAVAILABLE")
    ErrUpstreamTimeout     = errors.New("UPSTREAM_TIMEOUT")
)

