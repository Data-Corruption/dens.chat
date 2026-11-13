package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"time"
)

// clicksCommand is the delay by ear: it records this machine's microphone
// while the source's own output and the receiver's both play the click
// page's clicks, two seconds apart, and reads the delay off the pairs it
// hears, the source's click and then the relay's.
func clicksCommand(args []string) int {
	fs := flag.NewFlagSet("clicks", flag.ExitOnError)
	length := fs.Duration("for", 30*time.Second, "how long to listen")
	wav := fs.String("wav", "clicks-"+time.Now().Format("150405")+".wav", "where the recording is kept")
	loud := fs.Float64("loud", 0, "how loud a click is, of full scale; an eighth of the loudest moment unless given")
	_ = fs.Parse(args)
	quiet = true
	fmt.Printf("Listening to the microphone for %s...\n", *length)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *length)
	defer cancel()

	var all []int16
	var rate int
	capture(ctx, captureOptions{mic: true}, func(c Chunk) {
		if c.Filled {
			return
		}
		rate = c.Rate
		all = append(all, c.Frames...)
	})
	if rate == 0 || len(all) == 0 {
		fmt.Println("the microphone gave nothing")
		return 1
	}
	if err := writeWAV(*wav, all, rate); err != nil {
		fmt.Println(err)
	} else {
		fmt.Println("the recording:", *wav)
	}

	// Onsets: a loud frame after 30 ms without one, so the two clicks of a
	// pair stay apart and one click's ring doesn't count twice.
	peak := 0
	for i := 0; i < len(all)/2; i++ {
		a := int(all[2*i])
		if a < 0 {
			a = -a
		}
		peak = max(peak, a)
	}
	threshold := int(*loud * 32768)
	if *loud == 0 {
		threshold = peak / 8
	}
	var onsets []float64
	quietFor := rate
	for i := 0; i < len(all)/2; i++ {
		a := int(all[2*i])
		if a < 0 {
			a = -a
		}
		if a > threshold {
			if quietFor > rate*30/1000 {
				onsets = append(onsets, float64(i)/float64(rate))
			}
			quietFor = 0
		} else {
			quietFor++
		}
	}
	fmt.Printf("heard %.1f s at %d Hz, peak %.2f of full scale, %d clicks\n", float64(len(all)/2)/float64(rate), rate, float64(peak)/32768, len(onsets))
	var delays []float64
	for i := 1; i < len(onsets); i++ {
		gap := (onsets[i] - onsets[i-1]) * 1000
		fmt.Printf("  %8.3f s  +%.1f ms\n", onsets[i], gap)
		if gap < 1000 {
			delays = append(delays, gap)
		}
	}
	if len(delays) == 0 {
		fmt.Println("no pairs: one of the two outputs wasn't heard; the recording shows which")
		return 1
	}
	sort.Float64s(delays)
	fmt.Printf("the relay's click came after the source's by: min %.1f ms, median %.1f ms, max %.1f ms, over %d pairs\n",
		delays[0], delays[len(delays)/2], delays[len(delays)-1], len(delays))
	return 0
}

func writeWAV(path string, pcm []int16, rate int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	size := uint32(len(pcm) * 2)
	header := make([]byte, 44)
	copy(header[0:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:], 36+size)
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], 2)
	binary.LittleEndian.PutUint32(header[24:], uint32(rate))
	binary.LittleEndian.PutUint32(header[28:], uint32(rate*4))
	binary.LittleEndian.PutUint16(header[32:], 4)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], size)
	if _, err := f.Write(header); err != nil {
		return err
	}
	return binary.Write(f, binary.LittleEndian, pcm)
}
