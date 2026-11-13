//go:build !linux || android

package main

import "context"

func nullOutput(ctx context.Context) { logf("out", "-null-output is Linux's") }
