package internal

import "time"

type noopLogger struct{}

func (n noopLogger) Info(msg string, fields map[string]interface{})             {}
func (n noopLogger) Error(msg string, err error, fields map[string]interface{}) {}
func (n noopLogger) Debug(msg string, fields map[string]interface{})            {}

type noopMetrics struct{}

func (n noopMetrics) RecordLatency(strategy string, latency time.Duration) {}
func (n noopMetrics) RecordCacheHit(strategy string)                       {}
func (n noopMetrics) RecordCacheMiss(strategy string)                      {}
func (n noopMetrics) RecordError(strategy string, errType string)          {}
