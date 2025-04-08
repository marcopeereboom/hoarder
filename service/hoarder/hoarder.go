// Copyright (c) 2025 Marco Peereboom <marco@peereboom.us>
// Use of this source code is governed by the MIT License,
// which can be found in the LICENSE file.

package hoarder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/juju/loggo"
	"golang.org/x/sys/unix"
	"k8s.io/utils/mount"
)

const (
	logLevel = "INFO"
	appName  = "hoarder"

	defaultMeasurementDepth = 10000
	defaultLogLevel         = appName + "=INFO"
)

var (
	log = loggo.GetLogger(appName)

	defaultSubsystems = []string{
		"/proc/stat",
		"/proc/meminfo",
		"/proc/net/dev",
		"/proc/diskstats",
		"statfs[*]", // fsstat[mount point], * is all filesystems
	}
)

func init() {
	if err := loggo.ConfigureLoggers(logLevel); err != nil {
		panic(err)
	}
}

type FSStats struct {
	MountPoint  string `json:"mount_point"`
	BlockSize   uint64 `json:"block_size"`
	BlocksTotal uint64 `json:"blocks_total"`
	BlocksFree  uint64 `json:"blocks_free"` // worthless, use avail
	BlocksAvail uint64 `json:"blocks_available"`
}

type Measurement struct {
	Timestamp   int64  `json:"timestamp"`
	Subsystem   string `json:"subsystem"`
	Measurement string `json:"measurement"`
}

type Sink func(context.Context, []Measurement) error

type Config struct {
	Frequency  time.Duration // data collection frequency
	LogLevel   string
	Subsystems []string
}

func NewDefaultConfig() *Config {
	return &Config{
		Frequency:  5 * time.Second,
		LogLevel:   defaultLogLevel,
		Subsystems: defaultSubsystems,
	}
}

type Server struct {
	mtx sync.RWMutex
	wg  sync.WaitGroup

	cfg *Config

	measurementsC chan []Measurement

	externalSink Sink
}

func NewServer(cfg *Config) (*Server, error) {
	if cfg == nil {
		cfg = NewDefaultConfig()
	}

	s := &Server{
		cfg:           cfg,
		measurementsC: make(chan []Measurement, defaultMeasurementDepth),
	}

	return s, nil
}

func (s *Server) sink(ctx context.Context) error {
	log.Tracef("sink")
	defer log.Tracef("sink exit")

	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ms := <-s.measurementsC:
			if err := s.externalSink(ctx, ms); err != nil {
				return err
			}
		}
	}
}

func (s *Server) special(ctx context.Context, subsystem string) ([]byte, error) {
	log.Tracef("special")
	defer log.Tracef("special exit")

	ss := strings.SplitN(subsystem, "[", 2)
	if len(ss) <= 0 {
		return nil, fmt.Errorf("nothing to do")
	}
	switch ss[0] {
	case "statfs":
		if len(ss) < 2 {
			return nil, fmt.Errorf("statfs: malformed")
		}
		var (
			mounts []mount.MountPoint
			err    error
			b      bytes.Buffer
		)
		mountPoint := strings.Trim(ss[1], "[]")
		if mountPoint == "*" {
			mounts, err = NonVirtualMounts()
			if err != nil {
				return nil, fmt.Errorf("mounts: %w", err)
			}
		} else {
			mounts = append(mounts, mount.MountPoint{Path: mountPoint})
		}
		e := json.NewEncoder(&b)
		for k := range mounts {
			var stat unix.Statfs_t
			err := unix.Statfs(mounts[k].Path, &stat)
			if err != nil {
				return nil, fmt.Errorf("statfs: %w", err)
			}

			err = e.Encode(FSStats{
				MountPoint:  mounts[k].Path,
				BlockSize:   uint64(stat.Bsize),
				BlocksTotal: stat.Blocks,
				BlocksFree:  stat.Bfree,
				BlocksAvail: stat.Bavail,
			})
			if err != nil {
				return nil, fmt.Errorf("statfs: %w", err)
			}
		}

		return b.Bytes(), nil
	}

	return nil, fmt.Errorf("invalid special: %v", subsystem)
}

func (s *Server) run(ctx context.Context) error {
	log.Tracef("run")
	defer log.Tracef("run exit")

	defer s.wg.Done()

	ticker := time.NewTicker(s.cfg.Frequency)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			log.Infof("tick")

			ts := time.Now().Unix()
			ms := make([]Measurement, 0, len(s.cfg.Subsystems))
			for k := range s.cfg.Subsystems {
				var (
					m   []byte
					err error
				)
				if !strings.HasPrefix(s.cfg.Subsystems[k], "/") {
					m, err = s.special(ctx, s.cfg.Subsystems[k])
					if err != nil {
						return fmt.Errorf("special %v: %w",
							s.cfg.Subsystems[k], err)
					}
				} else {
					m, err = os.ReadFile(s.cfg.Subsystems[k])
					if err != nil {
						return fmt.Errorf("measurement %v: %w",
							s.cfg.Subsystems[k], err)
					}
				}
				ms = append(ms, Measurement{
					Timestamp:   ts,
					Subsystem:   s.cfg.Subsystems[k],
					Measurement: string(m),
				})
			}
			if len(ms) > 0 {
				select {
				case s.measurementsC <- ms:
				default:
					log.Debugf("dropped measurements at %v", ts)
				}
			}
		}
	}
}

func (s *Server) Run(pctx context.Context, es Sink) error {
	log.Tracef("Run")
	defer log.Tracef("Run exit")

	if es == nil {
		return fmt.Errorf("must provide sink")
	}
	s.externalSink = es

	ctx, cancel := context.WithCancel(pctx)
	defer cancel()

	errC := make(chan error)

	// ticker
	go func() {
		s.wg.Add(1)
		errC <- s.run(ctx)
	}()

	// sink
	go func() {
		s.wg.Add(1)
		errC <- s.sink(ctx)
	}()

	var err error
	select {
	case <-ctx.Done():
		err = ctx.Err()
	case err = <-errC:
	}
	cancel()

	log.Infof("hoarder service shutting down")
	s.wg.Wait()
	log.Infof("hoarder service clean shutdown")

	return err
}
