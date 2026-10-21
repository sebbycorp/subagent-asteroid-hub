// Command asteroids serves the Substrate Asteroids show for a kagent /
// Agent Substrate environment.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sebbycorp/subagent-asteroid-hub/internal/config"
	"github.com/sebbycorp/subagent-asteroid-hub/internal/hub"
	"github.com/sebbycorp/subagent-asteroid-hub/internal/kagent"
	"github.com/sebbycorp/subagent-asteroid-hub/internal/server"
	"github.com/sebbycorp/subagent-asteroid-hub/internal/tour"
	"github.com/sebbycorp/subagent-asteroid-hub/internal/traffic"
	"github.com/sebbycorp/subagent-asteroid-hub/web"
)

func main() {
	cfg, err := config.FromEnv(os.LookupEnv)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn := server.ConnInfo{API: cfg.API, Gateway: cfg.Gateway, UserID: cfg.UserID, Token: cfg.Token, CAFile: cfg.CAFile, Namespace: cfg.Namespace}
	client, err := dial(conn)
	if err != nil {
		log.Fatal(err)
	}

	h := hub.New(client, client.Env())
	gen := traffic.New(client, h, traffic.Options{
		Namespace: cfg.Namespace,
		Interval:  cfg.WaveInterval,
		OnStatus: func(auto bool, inFlight int, note string) {
			h.SetWaves(hub.WaveStatus{Auto: auto, InFlight: inFlight, Note: note})
		},
	})
	gen.SetAuto(cfg.AutoWaves)

	// Auto waves pause while the tour runs so each chapter's action is clear.
	var autoBefore bool
	guide := tour.New(client, gen, h, tour.Options{
		OnChange: func(s tour.Status) { h.SetTour(s) },
		OnActive: func(on bool) {
			if on {
				autoBefore = gen.Auto()
				gen.SetAuto(false)
			} else {
				gen.SetAuto(autoBefore)
			}
		},
	})
	h.SetTour(guide.Status())

	var mu sync.Mutex
	connect := func(c server.ConnInfo) error {
		next, err := dial(c)
		if err != nil {
			return err
		}
		mu.Lock()
		prev := client
		client = next
		mu.Unlock()
		h.SetSource(next, next.Env())
		gen.SetClient(next)
		guide.SetClient(next)
		prev.Close()
		log.Printf("switched to %s", next.Env())
		return nil
	}

	if cfg.DemoAgents {
		if names, err := gen.EnsureDemoAgents(ctx); err != nil {
			log.Printf("demo agents: %v", err)
		} else if len(names) > 0 {
			log.Printf("created demo agents %v", names)
		}
	}

	go h.Run(ctx, 250*time.Millisecond)
	go gen.Run(ctx)

	srv := &http.Server{Addr: cfg.Listen, Handler: server.New(h, gen, guide, connect, web.Files, conn, server.WithGatewaySink(h.Gateway))}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Printf("substrate asteroids on %s, kagent %s", cfg.Listen, cfg.API)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func dial(c server.ConnInfo) (*kagent.Client, error) {
	return kagent.Dial(kagent.Options{API: c.API, Gateway: c.Gateway, UserID: c.UserID, Token: c.Token, CAFile: c.CAFile, Namespace: c.Namespace})
}
