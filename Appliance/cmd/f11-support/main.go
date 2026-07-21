package main

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pzzzy/rongta-f11-open/appliance/internal/supportbundle"
)

type check struct {
	name string
	argv []string
}

type runner interface {
	CombinedOutput(context.Context, []string) ([]byte, error)
}
type execRunner struct{}

func (execRunner) CombinedOutput(ctx context.Context, argv []string) ([]byte, error) {
	return exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
}

var checks = []check{
	{"uname.txt", []string{"uname", "-a"}},
	{"release.txt", []string{"sh", "-c", "cat /etc/f11-image-release /usr/share/f11-image/version 2>/dev/null"}},
	{"systemd.txt", []string{"systemctl", "--no-pager", "--full", "status", "f11-setup-wizard", "f11-setup-helper", "cups"}},
	{"eventsub.txt", []string{"/usr/local/lib/f11-image/verify-eventsub"}},
	{"network.txt", []string{"nmcli", "-t", "general", "status"}},
	{"printer.txt", []string{"lpstat", "-v", "-p", "Rongta_F11_Media"}},
	{"usb.txt", []string{"sh", "-c", "/usr/lib/cups/backend/usb 2>&1"}},
	{"journal.txt", []string{"journalctl", "--no-pager", "-n", "500", "-u", "f11-first-boot", "-u", "f11-setup-wizard", "-u", "f11-setup-helper", "-u", "cups"}},
}

func collect(ctx context.Context, r runner, c check) []byte {
	out, err := r.CombinedOutput(ctx, c.argv)
	if err != nil {
		out = append(out, []byte("\ncommand_status=failed\n")...)
	}
	if len(out) > 512<<10 {
		out = out[:512<<10]
	}
	return []byte(supportbundle.RedactText(string(out)))
}
func main() {
	out := flag.String("output", "/var/lib/f11-setup/support.tar", "output tar path")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	tmp, err := os.CreateTemp(filepath.Dir(*out), ".support-*.tar")
	if err != nil {
		fatal(err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	tw := tar.NewWriter(tmp)
	for _, c := range checks {
		data := collect(ctx, execRunner{}, c)
		if found := supportbundle.ScanCanaries(data, strings.Fields(os.Getenv("F11_SUPPORT_CANARIES"))); len(found) > 0 {
			fatal(errors.New("support bundle canary detected"))
		}
		h := &tar.Header{Name: c.name, Mode: 0600, Size: int64(len(data)), ModTime: time.Unix(0, 0)}
		if err = tw.WriteHeader(h); err != nil {
			fatal(err)
		}
		if _, err = io.Copy(tw, bytes.NewReader(data)); err != nil {
			fatal(err)
		}
	}
	if err = tw.Close(); err != nil {
		fatal(err)
	}
	if err = tmp.Sync(); err != nil {
		fatal(err)
	}
	if err = tmp.Close(); err != nil {
		fatal(err)
	}
	if err = os.Chmod(name, 0600); err != nil {
		fatal(err)
	}
	if err = os.Rename(name, *out); err != nil {
		fatal(err)
	}
	fmt.Println(*out)
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "support bundle failed:", err); os.Exit(1) }
