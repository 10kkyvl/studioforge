package orclient

import (
	"fmt"
	"strings"
	"time"
)

type Kind string

const (
	KindAuthentication      Kind = "authentication"
	KindInsufficientCredits Kind = "insufficient_credits"
	KindRateLimited         Kind = "rate_limited"
	KindModelUnavailable    Kind = "model_unavailable"
	KindUnsupportedParams   Kind = "unsupported_parameters"
	KindContextOverflow     Kind = "context_overflow"
	KindProviderFailure     Kind = "provider_failure"
	KindNetworkFailure      Kind = "network_failure"
	KindCancelled           Kind = "cancelled"
	KindMalformedResponse   Kind = "malformed_response"
)

type APIError struct {
	Kind       Kind
	StatusCode int
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("openrouter: %s (%d): %s", e.Kind, e.StatusCode, e.Message)
}

func Action(k Kind) string {
	switch k {
	case KindAuthentication:
		return "Check your OpenRouter API key"
	case KindInsufficientCredits:
		return "Add credits to your OpenRouter account"
	case KindRateLimited:
		return "Wait and retry"
	case KindModelUnavailable:
		return "Choose another model or retry"
	case KindUnsupportedParams:
		return "Remove or adjust the unsupported request parameter"
	case KindContextOverflow:
		return "Reduce prompt/history size"
	case KindProviderFailure:
		return "Retry, or switch providers if the problem persists"
	case KindNetworkFailure:
		return "Check your network connection and retry"
	case KindCancelled:
		return "The request was cancelled"
	case KindMalformedResponse:
		return "Retry; the response could not be parsed"
	default:
		return "Retry"
	}
}

func classifyStatus(status int, message string) Kind {
	switch status {
	case 401:
		return KindAuthentication
	case 402:
		return KindInsufficientCredits
	case 429:
		return KindRateLimited
	case 403:
		return KindProviderFailure
	case 408:
		return KindNetworkFailure
	case 502, 503:
		return KindModelUnavailable
	case 400:
		return classifyBadRequest(message)
	}
	if status >= 500 {
		return KindProviderFailure
	}
	return KindProviderFailure
}

func classifyBadRequest(message string) Kind {
	m := strings.ToLower(message)
	if strings.Contains(m, "parameter") || strings.Contains(m, "unsupported") || strings.Contains(m, "not supported") || strings.Contains(m, "is not a valid") {
		return KindUnsupportedParams
	}
	if strings.Contains(m, "context_length") || strings.Contains(m, "context length") ||
		(strings.Contains(m, "context") && (strings.Contains(m, "token") || strings.Contains(m, "length") || strings.Contains(m, "window"))) ||
		(strings.Contains(m, "maximum") && strings.Contains(m, "token")) {
		return KindContextOverflow
	}
	return KindProviderFailure
}
