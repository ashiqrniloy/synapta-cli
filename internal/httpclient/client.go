// Package httpclient provides pre-configured *http.Client instances that
// should be used throughout the application instead of http.DefaultClient.
//
// # Why not http.DefaultClient?
//
// http.DefaultClient has no timeout configured: a stalled connection to any
// remote host will hang the entire process indefinitely.  Each call-site that
// rolled its own client also ended up with inconsistent settings (different
// timeouts, no connection pooling, …).
//
// # Client catalogue
//
//   - [Default]   – short-lived requests such as OAuth token exchange, update
//     checks, or any non-LLM HTTP call.  120 s overall timeout.
//   - [LLM]       – non-streaming LLM API calls (chat/completions,
//     responses).  300 s overall timeout, 120 s response-header timeout.
//   - [LLMStream] – streaming / SSE LLM calls.  No client-level timeout (the
//     stream stays open until the model is done).  Uses its own transport
//     with a 120 s response-header timeout so a server that never sends the
//     first byte is eventually abandoned.
//
// # Root cause of "http2: timeout awaiting response headers"
//
// The previous code shared one http.Transport between the streaming and
// non-streaming clients, and set ResponseHeaderTimeout to 30 s on that
// transport.  ResponseHeaderTimeout is the deadline for receiving *any*
// response header byte after the request has been sent — it is not specific
// to HTTP/2.  The error message "http2: timeout awaiting response headers"
// is simply Go's HTTP/2 layer surfacing the same timeout; an HTTP/1.1
// connection under the same condition would stall silently or produce a
// different error.
//
// Slow models (GitHub Copilot Codex, o-series reasoning models) spend
// significant time thinking before they can write the first token, so they
// routinely exceed 30 s before sending any response header.  The fix is:
//
//  1. Give [LLMStream] its own transport so its ResponseHeaderTimeout can
//     be tuned independently.
//  2. Raise that timeout to 120 s — long enough for slow models, short
//     enough to surface genuine server outages.
//
// HTTP/2 is kept enabled on all three clients.  It provides connection
// multiplexing, HPACK header compression, and reduced handshake latency
// for multi-turn conversations — all genuine benefits.  Disabling HTTP/2
// would not fix the underlying timeout problem and would regress
// performance, so it is intentionally not done here.
package httpclient

import (
	"net"
	"net/http"
	"time"
)

// ─── Timeout constants ────────────────────────────────────────────────────────

const (
	// defaultTimeout is the overall deadline for non-LLM requests.
	defaultTimeout = 120 * time.Second

	// llmTimeout is the overall deadline for non-streaming LLM calls.
	llmTimeout = 300 * time.Second

	// llmHeaderTimeout is how long we wait for response headers on both the
	// non-streaming LLM client and the streaming LLM client.  120 s is
	// intentionally generous: slow models (o-series, Codex) require
	// significant server-side processing before they can write the first
	// response byte.
	llmHeaderTimeout = 120 * time.Second

	// Common transport knobs.
	maxIdleConns        = 100
	maxIdleConnsPerHost = 10
	idleConnTimeout     = 90 * time.Second
	dialTimeout         = 30 * time.Second
	tlsHandshakeTimeout = 15 * time.Second
	keepAliveInterval   = 30 * time.Second
)

// ─── Shared clients ───────────────────────────────────────────────────────────

var (
	// Default is a general-purpose client for short-lived requests (OAuth,
	// update checks, etc.).  Do NOT use it for LLM API calls.
	Default *http.Client

	// LLM is used for non-streaming LLM API calls.
	LLM *http.Client

	// LLMStream is used for streaming / SSE LLM API calls.  It has its own
	// transport (separate from LLM and Default) so that its
	// ResponseHeaderTimeout can be tuned without affecting other clients.
	// HTTP/2 is kept enabled — it provides connection reuse and header
	// compression that benefit multi-turn LLM sessions.
	LLMStream *http.Client
)

func init() {
	dialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: keepAliveInterval,
	}

	// ── transport shared by Default and LLM ──────────────────────────────
	sharedTransport := &http.Transport{
		DialContext:           dialer.DialContext,
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: llmHeaderTimeout,
		ForceAttemptHTTP2:     true,
	}

	Default = &http.Client{
		Transport: sharedTransport,
		Timeout:   defaultTimeout,
	}

	LLM = &http.Client{
		Transport: sharedTransport,
		Timeout:   llmTimeout,
	}

	// ── dedicated transport for LLMStream ────────────────────────────────
	//
	// This transport is intentionally separate from sharedTransport.  The
	// only behavioural difference is that there is no client-level Timeout
	// on LLMStream (the stream stays open for the duration of model output),
	// but both transports carry the same generous ResponseHeaderTimeout so
	// neither will hang indefinitely waiting for a server that never responds.
	streamTransport := &http.Transport{
		DialContext:           dialer.DialContext,
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: llmHeaderTimeout,
		ForceAttemptHTTP2:     true,
	}

	// No client-level Timeout: the stream is open for as long as the model
	// keeps emitting tokens.  ResponseHeaderTimeout on the transport still
	// protects against servers that never send the first byte.
	LLMStream = &http.Client{
		Transport: streamTransport,
	}
}
