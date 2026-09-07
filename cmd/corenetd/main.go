// Command corenetd is the CoreNet node daemon.
//
// It serves the .core namespace over DNS, proxies HTTP requests to the
// registered services, offers a local control API to the corenet CLI, and
// exchanges directories with the statically configured nodes.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"corenet/internal/config"
	"corenet/internal/daemon"
	"corenet/pkg/protocol"
)

func main() {
	configPath := flag.String("config", "", "path to the configuration file")
	showVersion := flag.Bool("version", false, "print the CoreNet version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("corenetd %s\n", protocol.Version)
		return
	}

	logger := log.New(os.Stderr, "", log.LstdFlags)
	if err := run(*configPath, logger); err != nil {
		logger.Fatalf("corenetd: %v", err)
	}
}

func run(configPath string, logger *log.Logger) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if cfg.Path == "" {
		logger.Print("no configuration file found; using defaults")
	} else {
		logger.Printf("configuration: %s", cfg.Path)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	d := daemon.New(cfg, logger)
	go reloadOnHUP(ctx, d, cfg.Path, logger)

	return d.Run(ctx)
}

// reloadOnHUP re-reads the configuration file on SIGHUP.
func reloadOnHUP(ctx context.Context, d *daemon.Daemon, configPath string, logger *log.Logger) {
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)

	for {
		select {
		case <-ctx.Done():
			return
		case <-hup:
			if err := d.Reload(configPath); err != nil {
				logger.Printf("reload: %v", err)
			}
		}
	}
}
