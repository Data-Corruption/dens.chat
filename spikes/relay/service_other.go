//go:build !windows

package main

import "context"

func runService(ctx context.Context, o options) { fatal("-service is Windows'") }

func winSession() string { return "" }
