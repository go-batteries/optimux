package shared

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	_ "net/http/pprof"
)

type Profiler struct {
	state chan int
	addr  string
	once  sync.Once
}

func NewProfiler(addr string) *Profiler {
	return &Profiler{
		state: make(chan int, 1),
		addr:  addr,
		once:  sync.Once{},
	}
}

func (self *Profiler) Listen(ctx context.Context) {
	self.once.Do(func() {
		go self.loop(ctx)
	})
}

func (self *Profiler) On(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(20 * time.Second):
		return
	case self.state <- 1:
		log.Println("state toggle on")
	}
}

func (self *Profiler) Off(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(20 * time.Second):
		return
	case self.state <- 0:
		log.Println("state toggle off")
	}
}

func (p *Profiler) loop(ctx context.Context) {
	var srv *http.Server

	for {
		select {
		case <-ctx.Done():
			if srv == nil {
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			if err := srv.Shutdown(ctx); err != nil {
				log.Printf("[Profiler] shutdown error: %v", err)
			} else {
				log.Println("shutdown succesfully")
			}

			srv = nil

			return
		case sig := <-p.state:
			if sig == 1 && srv == nil {
				// Start server
				ln, err := net.Listen("tcp", p.addr)
				if err != nil {
					log.Printf("[Profiler] listen error: %v", err)
					continue
				}

				srv = &http.Server{Handler: http.DefaultServeMux}

				go func() {
					log.Println("[Profiler] running at", p.addr)
					if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
						log.Printf("[Profiler] server error: %v", err)
					}
				}()
			}

			if sig == 0 && srv != nil {
				log.Println("[Profiler] shutting down")
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()

				if err := srv.Shutdown(ctx); err != nil {
					log.Printf("[Profiler] shutdown error: %v", err)
				} else {
					log.Println("shutdown succesfully")
				}

				srv = nil
			}
		}
	}
}
