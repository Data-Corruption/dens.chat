// Command sizeprobe measures what the translated module adds to a binary:
// built with -tags linked it links the module, and without it, it doesn't.
package main

import "fmt"

// Printing the function's address keeps it, and all it reaches, in the binary.
func main() { fmt.Printf("%p\n", link()) }
