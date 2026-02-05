package vectron

// Help returns a human-readable help string and a structured options map.
func Help() (string, map[string]any) {
	helpText := `Vectron Go Client Help

Constructor:
- NewClient(host, jwtToken)
- NewClientWithOptions(host, jwtToken, options)

Options (ClientOptions):
- Timeout: per-RPC timeout (default 10s, set < 0 to disable)
- DialTimeout: connection timeout (default 10s)
- MaxRecvMsgSize: max inbound message bytes (default 16MB)
- MaxSendMsgSize: max outbound message bytes (default 16MB)
- KeepaliveTime: client keepalive ping interval (default 30s)
- KeepaliveTimeout: keepalive ack timeout (default 10s)
- KeepalivePermitWithoutStream: allow keepalive with no active streams (default false)
- UseTLS: enable TLS (default false)
- TLSConfig: custom TLS config (optional)
- TLSServerName: SNI override (optional)
- ExpectedVectorDim: validate vector length when > 0 (default 0, disabled)
- UserAgent: custom gRPC user agent (default "vectron-go-client")
- RetryPolicy.MaxAttempts: max attempts including first (default 3)
- RetryPolicy.InitialBackoff: base backoff (default 100ms)
- RetryPolicy.MaxBackoff: max backoff (default 2s)
- RetryPolicy.BackoffMultiplier: exponential factor (default 2)
- RetryPolicy.Jitter: random jitter fraction (default 0.2)
- RetryPolicy.RetryOnWrites: retry non-idempotent ops (default false)
- RetryPolicy.RetryOnCodes: retryable gRPC codes (default Unavailable, DeadlineExceeded, ResourceExhausted)
- Compression: gRPC compression (default disabled, set "gzip")
- HedgedReads: enable duplicate reads to reduce tail latency (default false)
- HedgeDelay: delay before hedged read (default 75ms)
Batch options (BatchOptions):
- BatchSize: max points per batch (default 256)
- MaxBatchBytes: max batch payload size in bytes (approximate, default disabled)
- Concurrency: concurrent batch requests (default 1)

Safety defaults:
- Timeouts enabled
- Max message size capped
- Input validation on vector length (optional)
- Retries enabled for read-only operations only

Performance defaults:
- Reused gRPC connection
- Keepalive enabled with conservative intervals
`

	options := map[string]any{
		"Timeout":                      defaultTimeout,
		"DialTimeout":                  defaultDialTimeout,
		"MaxRecvMsgSize":               defaultMaxMsgSize,
		"MaxSendMsgSize":               defaultMaxMsgSize,
		"KeepaliveTime":                defaultKeepaliveTime,
		"KeepaliveTimeout":             defaultKeepaliveTO,
		"KeepalivePermitWithoutStream": false,
		"UseTLS":                       false,
		"TLSConfig":                    "(*tls.Config)(optional)",
		"TLSServerName":                "",
		"ExpectedVectorDim":            0,
		"UserAgent":                    defaultUserAgent,
		"RetryPolicy": map[string]any{
			"MaxAttempts":       3,
			"InitialBackoff":    "100ms",
			"MaxBackoff":        "2s",
			"BackoffMultiplier": 2,
			"Jitter":            0.2,
			"RetryOnWrites":     false,
			"RetryOnCodes":      []string{"Unavailable", "DeadlineExceeded", "ResourceExhausted"},
		},
		"Compression": "disabled",
		"HedgedReads": false,
		"HedgeDelay":  "75ms",
		"Notes": map[string]string{
			"timeout_disable": "Set Timeout < 0 to disable per-RPC deadlines.",
			"tls":             "Use TLS in production for secure transport.",
			"retries":         "Retries apply to read-only operations unless RetryOnWrites is enabled.",
			"hedged_reads":    "Hedged reads can reduce tail latency but may increase load.",
		},
	}

	return helpText, options
}
