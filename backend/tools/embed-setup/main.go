// Command embed-setup warms the fastembed model cache so the daemon
// never downloads at runtime. Optional: on failure the daemon runs in
// keyword+graph recall (degraded) mode.
package main

import (
	"fmt"
	"os"

	"zbot/backend/chitta"
)

func main() {
	e, err := chitta.NewFastEmbedder("BAAI/bge-small-en-v1.5", chitta.DefaultEmbedModelDir())
	if err != nil {
		fmt.Fprintln(os.Stderr, "embed-setup:", err)
		os.Exit(1)
	}
	_ = e.Close()
	fmt.Println("embed model cache warm")
}
