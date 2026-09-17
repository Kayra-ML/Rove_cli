package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/aether-dev/aether/internal/config"
	"github.com/aether-dev/aether/internal/daemon"
)

func main() {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := cfg.EnsureDirs(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	d, err := daemon.Start(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = daemon.WritePID(cfg.DataDir)
	fmt.Printf("aetherd listening http=%s ipc=%s data=%s\n", cfg.ListenHTTP, cfg.ListenIPC, cfg.DataDir)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	_ = d.Stop()
}
