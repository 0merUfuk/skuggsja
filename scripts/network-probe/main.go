// Command network-probe is a negative control for the macOS runtime guard.
// It is verification-only and is never included in Skuggsja release builds.
package main

import (
	"net"
	"os"
	"time"
)

func main() {
	connection, err := net.DialTimeout("tcp", "93.184.216.34:80", time.Second)
	if err != nil {
		return
	}
	_ = connection.Close()
	os.Exit(1)
}
