package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"golang.ngrok.com/ngrok/v2"
)

type EmbeddedNgrok struct {
	publicURL string
	agent     ngrok.Agent
	listener  ngrok.EndpointListener
	server    *http.Server
}

func (e *EmbeddedNgrok) PublicURL() string {
	if e == nil {
		return ""
	}
	return e.publicURL
}

func (e *EmbeddedNgrok) Close(ctx context.Context) error {
	if e == nil {
		return nil
	}

	var closeErr error

	if e.server != nil {
		if err := e.server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			closeErr = errors.Join(closeErr, err)
		}
	}

	if e.listener != nil {
		if err := e.listener.CloseWithContext(ctx); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}

	if e.agent != nil {
		if err := e.agent.Disconnect(); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}

	return closeErr
}

func StartEmbeddedNgrok(ctx context.Context, handler http.Handler) (*EmbeddedNgrok, error) {
	token := strings.TrimSpace(os.Getenv("NGROK_AUTHTOKEN"))
	if token == "" {
		return nil, nil
	}

	agent, err := ngrok.NewAgent(
		ngrok.WithAuthtoken(token),
		ngrok.WithAgentDescription("Remainwith embedded tunnel"),
		ngrok.WithAgentMetadata("service=remainwith"),
	)
	if err != nil {
		return nil, fmt.Errorf("create ngrok agent: %w", err)
	}

	endpointOpts := []ngrok.EndpointOption{
		ngrok.WithDescription("Remainwith local development server"),
		ngrok.WithMetadata("service=remainwith,transport=http"),
	}

	if urlSpec := resolveNgrokURL(); urlSpec != "" {
		endpointOpts = append(endpointOpts, ngrok.WithURL(urlSpec))
	}

	listener, err := agent.Listen(ctx, endpointOpts...)
	if err != nil {
		_ = agent.Disconnect()
		return nil, fmt.Errorf("start ngrok listener: %w", err)
	}

	ngrokServer := &http.Server{
		Handler: handler,
	}

	go func() {
		if err := ngrokServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Embedded ngrok server stopped with error: %v", err)
		}
	}()

	return &EmbeddedNgrok{
		publicURL: listener.URL().String(),
		agent:     agent,
		listener:  listener,
		server:    ngrokServer,
	}, nil
}

func StartEmbeddedNgrokAsync(handler http.Handler) <-chan string {
	urlChan := make(chan string, 1)

	token := strings.TrimSpace(os.Getenv("NGROK_AUTHTOKEN"))
	if token == "" {
		log.Printf("Embedded ngrok skipped: NGROK_AUTHTOKEN is not set")
		close(urlChan)
		return urlChan
	}

	log.Printf("Embedded ngrok enabled: starting tunnel in background")

	go func() {
		defer close(urlChan)

		embeddedNgrok, err := StartEmbeddedNgrok(context.Background(), handler)
		if err != nil {
			log.Printf("Embedded ngrok startup failed: %v", err)
			return
		}
		if embeddedNgrok == nil {
			log.Printf("Embedded ngrok skipped: no tunnel created")
			return
		}

		publicURL := embeddedNgrok.PublicURL()
		urlChan <- publicURL
	}()

	return urlChan
}

func resolveNgrokURL() string {
	if urlSpec := strings.TrimSpace(os.Getenv("NGROK_URL")); urlSpec != "" {
		return urlSpec
	}

	if domain := strings.TrimSpace(os.Getenv("NGROK_DOMAIN")); domain != "" {
		if strings.HasPrefix(domain, "http://") || strings.HasPrefix(domain, "https://") {
			return domain
		}
		return "https://" + domain
	}

	return ""
}
