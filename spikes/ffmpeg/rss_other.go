//go:build !linux && !windows

package main

import "os"

func peakRSSMB(*os.ProcessState) float64 { return 0 }

func selfPeakMB() float64 { return 0 }
