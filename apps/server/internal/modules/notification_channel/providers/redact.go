package providers

import (
	"net/url"
	"strconv"
	"strings"
)

// redactedURL replaces a request URL in an error message
const redactedURL = "[redacted URL]"

// RedactURLErrors returns err with the request URL of any *url.Error in its chain removed
// from its text, or err itself when the chain holds none. An HTTP client puts the whole
// request URL into its errors, and for a notification channel the URL holds a credential
// (a Telegram bot token, a webhook URL), so errors leave the notification module through
// this function.
//
// The message of a "failed to send: %w" wrapper already contains the URL text, so unwrapping
// the *url.Error isn't enough; the returned error carries the rebuilt message. It still
// unwraps to err, so errors.Is and errors.As keep working for the caller (a timeout stays a
// timeout), but only its text is safe to show or log.
func RedactURLErrors(err error) error {
	if err == nil {
		return nil
	}

	urlErrs := collectURLErrors(err, nil)
	if len(urlErrs) == 0 {
		return err
	}

	// An outer *url.Error holds an inner one in its Err, so redacting the outer one first
	// keeps the inner one's text intact for its own turn
	msg := err.Error()
	for _, urlErr := range urlErrs {
		msg = redactURLError(msg, urlErr)
	}
	return &redactedError{msg: msg, err: err}
}

// collectURLErrors appends every *url.Error in err's chain to found, outermost first
func collectURLErrors(err error, found []*url.Error) []*url.Error {
	if err == nil {
		return found
	}
	if urlErr, ok := err.(*url.Error); ok {
		found = append(found, urlErr)
	}
	switch e := err.(type) {
	case interface{ Unwrap() error }:
		return collectURLErrors(e.Unwrap(), found)
	case interface{ Unwrap() []error }:
		for _, inner := range e.Unwrap() {
			found = collectURLErrors(inner, found)
		}
	}
	return found
}

// redactURLError removes urlErr's URL from msg. A *url.Error prints as `Op "URL": cause`
// (the URL quoted), so that whole text is swapped for `Op [redacted URL]: cause`. If a
// wrapper reformatted the text, the quoted and the raw URL are replaced wherever they
// still appear.
func redactURLError(msg string, urlErr *url.Error) string {
	if urlErr.URL == "" {
		return msg
	}

	cause := ""
	if urlErr.Err != nil {
		cause = urlErr.Err.Error()
	}
	msg = strings.ReplaceAll(msg, urlErr.Error(), urlErr.Op+" "+redactedURL+": "+cause)
	msg = strings.ReplaceAll(msg, strconv.Quote(urlErr.URL), redactedURL)
	return strings.ReplaceAll(msg, urlErr.URL, redactedURL)
}

// redactedError is an error whose text has been scrubbed of request URLs
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }

func (e *redactedError) Unwrap() error { return e.err }
