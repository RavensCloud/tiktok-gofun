package tiktok

import "errors"

var (
	ErrRateLimited    = errors.New("tiktok: rate limited")
	ErrNotFound       = errors.New("tiktok: not found")
	ErrAuthRequired   = errors.New("tiktok: authentication required")
	ErrCaptcha        = errors.New("tiktok: captcha required")
	ErrSigningFailed  = errors.New("tiktok: url signing failed")
	ErrBrowserNotReady = errors.New("tiktok: browser not initialized")
	ErrInvalidResponse = errors.New("tiktok: invalid response")
)
