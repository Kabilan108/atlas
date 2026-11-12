package pipeline

import (
	"cmp"
	"errors"
	"slices"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

const redacted = "[redacted]"

// securedValues are the values of the secured variables in a run request,
// longest first so a value containing another is replaced whole.
func securedValues(variables []Variable) []string {
	var values []string
	for _, v := range variables {
		if v.Secured && v.Value != "" {
			values = append(values, v.Value)
		}
	}
	slices.SortFunc(values, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	return slices.Compact(values)
}

// secretsRedactedError hides secured variable values from an error's text.
// Bitbucket may echo the request body in a validation error, so the API
// error in the chain is scrubbed too: front ends reach it with errors.As and
// still classify it by status.
type secretsRedactedError struct {
	err     error
	secrets []string
}

func redactSecrets(err error, secrets []string) error {
	if err == nil || len(secrets) == 0 {
		return err
	}
	var apiErr *bitbucket.APIError
	if errors.As(err, &apiErr) {
		apiErr.Message = redact(apiErr.Message, secrets)
		apiErr.Resource = redact(apiErr.Resource, secrets)
		apiErr.Hint = redact(apiErr.Hint, secrets)
	}
	return &secretsRedactedError{err: err, secrets: secrets}
}

func (e *secretsRedactedError) Error() string {
	return redact(e.err.Error(), e.secrets)
}

func (e *secretsRedactedError) Unwrap() error {
	return e.err
}

func redact(text string, secrets []string) string {
	for _, secret := range secrets {
		text = strings.ReplaceAll(text, secret, redacted)
	}
	return text
}
