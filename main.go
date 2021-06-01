package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

func main() {
	address := os.Getenv("TEMOVEX_ADDR")

	var g run.Group
	{
		// Handle os signals
		done := make(chan struct{})

		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		g.Add(
			func() error {
				select {
				case s := <-ch:
					log.WithFields(log.Fields{
						"signal": s.String(),
					}).Info("Received os signal, exiting gracefully...")
				case <-done:
					break
				}
				return nil
			},
			func(err error) {
				close(done)
			},
		)
	}
	{
		log.Info("Listening to http://0.0.0.0:8080")
		ln, err := net.Listen("tcp", ":8080")
		if err != nil {
			log.WithFields(log.Fields{
				"error": err.Error(),
			}).Info("An error occurred while opening socket")
			os.Exit(1)
		}
		defer ln.Close()

		r := chi.NewRouter()
		r.Use(middleware.Heartbeat("/ready"))
		r.Use(middleware.Heartbeat("/health"))

		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("temovex-exporter"))
		})

		p := prometheus.NewRegistry()
		p.MustRegister(newCollector(address))
		r.Handle("/metrics", promhttp.HandlerFor(p, promhttp.HandlerOpts{}))

		srv := http.Server{Handler: r}
		g.Add(
			func() error {
				return srv.Serve(ln)
			},
			func(err error) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				srv.Shutdown(ctx)
			},
		)
	}

	if err := g.Run(); err != nil {
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("rungroup recieve error")
		os.Exit(1)
	}

}
