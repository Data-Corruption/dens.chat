// Command aprobe answers the Android spike's questions about the system
// under the shell, from inside the app's sandbox, on the emulator and the
// phones. The shell carries it as lib/<abi>/libprobe.so, beside Dens, and
// runs it from the native library directory as it runs Dens. Where Dens has
// a seam already (locked memory, low-priority children, the control
// endpoint) it runs that seam, so what works here works for Dens.
//
// It writes into its working directory, which the shell sets to the app's
// spike directory: report.txt, heartbeat.log, burn.txt and features.json.
package main

import (
	"fmt"
	"os"
	"time"
)

// version names the build, which build.sh sets.
var version = "dev"

const usage = `aprobe COMMAND [FLAGS]

  report [--key-stdin] [--lan HOST:PORT]
                       the system, the seams and the network, to report.txt
  heartbeat            a line a second to heartbeat.log, until killed
  gaps                 the heartbeat's gaps: when the probe froze, slept or died
  burn N SECONDS       N low-priority children spinning, as media workers would
  serve PORT           the WebView's feature page, on localhost
  syscall NAME         one system call, in a child the report starts
  spin SECONDS         one busy child, which burn starts
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch args := os.Args[2:]; os.Args[1] {
	case "report":
		err = report(args)
	case "heartbeat":
		err = heartbeat()
	case "gaps":
		err = gaps(os.Stdout)
	case "burn":
		err = burn(args)
	case "serve":
		err = serve(args)
	case "syscall":
		err = syscallChild(args)
	case "spin":
		err = spin(args)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s aprobe %s: %v\n", time.Now().Format(time.RFC3339), os.Args[1], err)
		os.Exit(1)
	}
}
