//go:build !linked

package main

func link() func() { return nil }
