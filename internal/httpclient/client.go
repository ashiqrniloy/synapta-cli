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
//
// # Optional request tracing
//
// Set SYNAPTA_HTTP_TRACE=1 to attach low-noise net/http/httptrace diagnostics
// to all HTTP clients in this package. Tracing intentionally prints only on
// failed requests and focuses on where the request likely failed (DNS,
// connect, TLS, or waiting for response headers).
package httpclient

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"strings"
	"sync/atomic"
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

	traceEnvVar = "SYNAPTA_HTTP_TRACE"
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

	httpTraceEnabled = parseTraceEnabled(os.Getenv(traceEnvVar))
	traceCounter     uint64
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
		Transport: traceRoundTripper("default", sharedTransport),
		Timeout:   defaultTimeout,
	}

	LLM = &http.Client{
		Transport: traceRoundTripper("llm", sharedTransport),
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
		Transport: traceRoundTripper("llm-stream", streamTransport),
	}
}

type tracingTransport struct {
	name string
	next http.RoundTripper
}

func traceRoundTripper(name string, next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return &tracingTransport{name: name, next: next}
}

func (t *tracingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !httpTraceEnabled || req == nil {
		return t.next.RoundTrip(req)
	}

	trace := newHTTPTraceRecord(t.name, req)
	tracedReq := req.Clone(httptrace.WithClientTrace(req.Context(), trace.clientTrace()))

	resp, err := t.next.RoundTrip(tracedReq)
	if err != nil {
		return nil, trace.wrapError(err)
	}
	return resp, nil
}

type httpTraceRecord struct {
	id       string
	client   string
	method   string
	target   string
	started  time.Time
	gotConn  time.Time
	wroteReq time.Time
	firstB   time.Time

	connReused bool
	connWasIdle bool
	connIdleFor time.Duration
	network     string
	addr        string
	dnsErr      string
	connectErr  string
	tlsErr      string
}

func newHTTPTraceRecord(client string, req *http.Request) *httpTraceRecord {
	target := ""
	if req != nil && req.URL != nil {
		target = req.URL.Scheme + "://" + req.URL.Host + req.URL.EscapedPath()
	}
	return &httpTraceRecord{
		id:      nextTraceID(),
		client:  client,
		method:  req.Method,
		target:  target,
		started: time.Now(),
	}
}

func (t *httpTraceRecord) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			t.gotConn = time.Now()
			t.connReused = info.Reused
			t.connWasIdle = info.WasIdle
			t.connIdleFor = info.IdleTime
			if info.Conn != nil {
				t.network = info.Conn.RemoteAddr().Network()
				t.addr = info.Conn.RemoteAddr().String()
			}
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			if info.Err != nil {
				t.dnsErr = info.Err.Error()
			}
		},
		ConnectDone: func(network, addr string, err error) {
			t.network = network
			t.addr = addr
			if err != nil {
				t.connectErr = err.Error()
			}
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			if err != nil {
				t.tlsErr = err.Error()
			}
		},
		WroteRequest: func(_ httptrace.WroteRequestInfo) {
			t.wroteReq = time.Now()
		},
		GotFirstResponseByte: func() {
			t.firstB = time.Now()
		},
	}
}

func (t *httpTraceRecord) wrapError(err error) error {
	elapsed := time.Since(t.started).Round(time.Millisecond)
	phase := "before write"

	switch {
	case t.dnsErr != "":
		phase = "dns"
	case t.connectErr != "":
		phase = "connect"
	case t.tlsErr != "":
		phase = "tls"
	case !t.wroteReq.IsZero() && t.firstB.IsZero():
		phase = "awaiting response headers"
	case !t.firstB.IsZero():
		phase = "response body/read"
	case !t.gotConn.IsZero():
		phase = "request write"
	}

	conn := "new"
	if t.connReused {
		conn = "reused"
	}

	hint := ""
	if strings.Contains(strings.ToLower(err.Error()), "timeout awaiting response headers") {
		hint = " hint=response_header_timeout"
	}

	msg := fmt.Sprintf(
		"http trace id=%s client=%s req=%s %s phase=%s elapsed=%s conn=%s net=%s addr=%s%s",
		t.id,
		t.client,
		t.method,
		t.target,
		phase,
		elapsed,
		conn,
		t.network,
		t.addr,
		hint,
	)
	if t.connWasIdle {
		msg += fmt.Sprintf(" idle_for=%s", t.connIdleFor.Round(time.Millisecond))
	}
	if t.dnsErr != "" {
		msg += " dns_err=" + quoteIfNeeded(t.dnsErr)
	}
	if t.connectErr != "" {
		msg += " connect_err=" + quoteIfNeeded(t.connectErr)
	}
	if t.tlsErr != "" {
		msg += " tls_err=" + quoteIfNeeded(t.tlsErr)
	}

	return fmt.Errorf("%s: %w", msg, err)
}

func parseTraceEnabled(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "debug", "trace":
		return true
	default:
		return false
	}
}

func nextTraceID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	v := atomic.AddUint64(&traceCounter, 1)
	return fmt.Sprintf("%08x", v)
}

func quoteIfNeeded(v string) string {
	if strings.ContainsAny(v, " \t") {
		return fmt.Sprintf("%q", v)
	}
	return v
}
