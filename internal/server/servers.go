package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/time/rate"
)

type Servers struct {
	Logs func(lg string, v ...any)
}

func (server Servers) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	con, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{"echo"},
	})
	if err != nil {
		server.Logs("websocket accept error: %v", err)
		return
	}
	defer con.CloseNow()

	if con.Subprotocol() != "echo" {
		con.Close(websocket.StatusPolicyViolation, "client must speak the echo subprotocol")
		return
	}
	limit := rate.NewLimiter(rate.Every(time.Millisecond*100), 10)
	for {
		err = echo(con, limit)
		if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
			return
		}
		if err != nil {
			server.Logs("failed to echo with %s: %v", r.RemoteAddr, err)
			return
		}
	}
}

func echo(con *websocket.Conn, limit *rate.Limiter) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	err := limit.Wait(ctx)
	if err != nil {
		return err
	}

	tp, r, err := con.Reader(ctx)
	if err != nil {
		return err
	}
	w, err := con.Writer(ctx, tp)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	if err != nil {
		return fmt.Errorf("failed to io.copy:%w", err)
	}
	err = w.Close()
	return err

}
func run() error {
	if len(os.Args) < 2 {
		return errors.New("please provide an address to listen on as the first argument")
	}

	l, err := net.Listen("tcp", os.Args[1])
	if err != nil {
		return err
	}
	log.Printf("listening on ws://%v", l.Addr())

	s := &http.Server{
		Handler: Servers{

			Logs: log.Printf,
		},
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}
	errc := make(chan error, 1)
	go func() {
		errc <- s.Serve(l)
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	select {
	case err := <-errc:
		log.Printf("failed to serve: %v", err)
	case sig := <-sigs:
		log.Printf("terminating: %v", sig)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	return s.Shutdown(ctx)
}
