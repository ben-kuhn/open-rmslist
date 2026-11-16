package browser

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Open launches the default web browser at url. It returns immediately.
func Open(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default: // linux, freebsd, openbsd, netbsd, ...
		for _, c := range []string{"xdg-open", "sensible-browser", "x-www-browser"} {
			if _, err := exec.LookPath(c); err == nil {
				cmd = exec.Command(c, url)
				break
			}
		}
		if cmd == nil {
			return fmt.Errorf("no browser launcher found on PATH")
		}
	}
	return cmd.Start()
}
