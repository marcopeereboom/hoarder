// Copyright (c) 2025 Marco Peereboom <marco@peereboom.us>
// Use of this source code is governed by the MIT License,
// which can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/juju/loggo"

	"github.com/hemilabs/heminetwork/config"
	"github.com/marcopeereboom/hoarder/service/hoarder"
	"github.com/marcopeereboom/hoarder/version"
)

const (
	daemonName      = "hoarderd"
	defaultLogLevel = daemonName + "=INFO;hoarder=INFO;level=INFO"
)

var (
	welcome string

	log = loggo.GetLogger(daemonName)
	cfg = hoarder.NewDefaultConfig()
	cm  = config.CfgMap{
		"HOARDER_FREQUENCY": config.Config{
			Value:        &cfg.Frequency,
			DefaultValue: 5 * time.Second,
			Help:         "polling frequency",
			Print:        config.PrintAll,
		},
		"HOARDER_LOG_LEVEL": config.Config{
			Value:        &cfg.LogLevel,
			DefaultValue: defaultLogLevel,
			Help:         "loglevel for various packages; INFO, DEBUG and TRACE",
			Print:        config.PrintAll,
		},
	}
)

func HandleSignals(ctx context.Context, cancel context.CancelFunc, callback func(os.Signal)) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	defer func() {
		signal.Stop(signalChan)
		cancel()
	}()

	select {
	case <-ctx.Done():
	case s := <-signalChan: // First signal, cancel context.
		if callback != nil {
			callback(s) // Do whatever caller wants first.
			cancel()
		}
	}
	<-signalChan // Second signal, hard exit.
	os.Exit(2)
}

func _main() error {
	// Parse configuration from environment
	if err := config.Parse(cm); err != nil {
		return err
	}

	if err := loggo.ConfigureLoggers(cfg.LogLevel); err != nil {
		return err
	}
	log.Infof("%v", welcome)

	pc := config.PrintableConfig(cm)
	for k := range pc {
		log.Infof("%v", pc[k])
	}

	ctx, cancel := context.WithCancel(context.Background())
	go HandleSignals(ctx, cancel, func(s os.Signal) {
		log.Infof("hoarder service received signal: %s", s)
	})
	server, err := hoarder.NewServer(cfg)
	if err != nil {
		return fmt.Errorf("create hoarder server: %w", err)
	}

	if err := server.Run(ctx); !errors.Is(err, context.Canceled) {
		return fmt.Errorf("hoarder server terminated: %w", err)
	}

	return nil
}

func init() {
	version.Component = "hoarderd"
	welcome = "Hoarder daemon " + version.BuildInfo()
}

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintf(os.Stderr, "%v\n", welcome)
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "\thelp (this help)\n")
		fmt.Fprintf(os.Stderr, "Environment:\n")
		config.Help(os.Stderr, cm)
		os.Exit(1)
	}

	if err := _main(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
