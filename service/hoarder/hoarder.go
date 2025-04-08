// Copyright (c) 2025 Marco Peereboom <marco@peereboom.us>
// Use of this source code is governed by the MIT License,
// which can be found in the LICENSE file.

package hoarder

import (
	"context"
	"sync"
	"time"

	"github.com/juju/loggo"
)

const (
	logLevel = "INFO"
	appName  = "hoarder"
)

var log = loggo.GetLogger(appName)

func init() {
	if err := loggo.ConfigureLoggers(logLevel); err != nil {
		panic(err)
	}
}

type Config struct {
	Frequency time.Duration // data collection frequency
	LogLevel  string
}

func NewDefaultConfig() *Config {
	return &Config{
		Frequency: 5 * time.Second,
	}
}

type Server struct {
	mtx sync.RWMutex
	wg  sync.WaitGroup

	cfg *Config
}

func NewServer(cfg *Config) (*Server, error) {
	if cfg == nil {
		cfg = NewDefaultConfig()
	}

	s := &Server{
		cfg: cfg,
	}

	return s, nil
}

func (s *Server) Run(pctx context.Context) error {
	log.Tracef("Run")
	defer log.Tracef("Run exit")

	ctx, cancel := context.WithCancel(pctx)
	defer cancel()

	var err error
	select {
	case <-ctx.Done():
		err = ctx.Err()
	}
	cancel()

	log.Infof("hoarder service shutting down")
	s.wg.Wait()
	log.Infof("hoarder service clean shutdown")

	return err
}
