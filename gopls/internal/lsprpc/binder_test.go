// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lsprpc_test

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/gopls/internal/protocol"
	jsonrpc2_v2 "golang.org/x/tools/internal/jsonrpc2_v2"

	. "golang.org/x/tools/gopls/internal/lsprpc"
)

// ServerBinder binds incoming connections to a new server.
type ServerBinder struct {
	newServer ServerFunc
}

func NewServerBinder(newServer ServerFunc) *ServerBinder {
	return &ServerBinder{newServer: newServer}
}

// streamServer used to have this method, but it was never used.
// TODO(adonovan): figure out whether we need any of this machinery
// and, if not, delete it. In the meantime, it's better that it sit
// in the test package with all the other mothballed machinery
// than in the production code where it would couple streamServer
// and ServerBinder.
/*
func (s *streamServer) Binder() *ServerBinder {
	newServer := func(ctx context.Context, client protocol.ClientCloser) protocol.Server {
		session := cache.NewSession(ctx, s.cache)
		svr := s.serverForTest
		if svr == nil {
			options := settings.DefaultOptions(s.optionsOverrides)
			svr = server.New(session, client, options)
			if instance := debug.GetInstance(ctx); instance != nil {
				instance.AddService(svr, session)
			}
		}
		return svr
	}
	return NewServerBinder(newServer)
}
*/

func (b *ServerBinder) Bind(ctx context.Context, conn *jsonrpc2_v2.Connection) jsonrpc2_v2.ConnectionOptions {
	client := protocol.ClientDispatcherV2(conn)
	server := b.newServer(ctx, client)
	serverHandler := protocol.ServerHandlerV2(server)
	// Wrap the server handler to inject the client into each request context, so
	// that log events are reflected back to the client.
	wrapped := jsonrpc2_v2.HandlerFunc(func(ctx context.Context, req *jsonrpc2_v2.Request) (any, error) {
		ctx = protocol.WithClient(ctx, client)
		return serverHandler.Handle(ctx, req)
	})
	preempter := &Canceler{
		Conn: conn,
	}
	return jsonrpc2_v2.ConnectionOptions{
		Handler:   wrapped,
		Preempter: preempter,
	}
}

type TestEnv struct {
	Conns   []*jsonrpc2_v2.Connection
	Servers []*jsonrpc2_v2.Server
}

func (e *TestEnv) Shutdown(t *testing.T) {
	for _, s := range e.Servers {
		s.Shutdown()
	}
	for _, c := range e.Conns {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	}
	for _, s := range e.Servers {
		if err := s.Wait(); err != nil {
			t.Error(err)
		}
	}
}

func (e *TestEnv) serve(ctx context.Context, t *testing.T, server jsonrpc2_v2.Binder) (jsonrpc2_v2.Listener, *jsonrpc2_v2.Server) {
	l, err := jsonrpc2_v2.NetPipeListener(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s := jsonrpc2_v2.NewServer(ctx, l, server)
	e.Servers = append(e.Servers, s)
	return l, s
}

func (e *TestEnv) dial(ctx context.Context, t *testing.T, dialer jsonrpc2_v2.Dialer, client jsonrpc2_v2.Binder, forwarded bool) *jsonrpc2_v2.Connection {
	if forwarded {
		l, _ := e.serve(ctx, t, NewForwardBinder(dialer))
		dialer = l.Dialer()
	}
	conn, err := jsonrpc2_v2.Dial(ctx, dialer, client, nil)
	if err != nil {
		t.Fatal(err)
	}
	e.Conns = append(e.Conns, conn)
	return conn
}

func staticClientBinder(client protocol.Client) jsonrpc2_v2.Binder {
	f := func(context.Context, protocol.Server) protocol.Client { return client }
	return NewClientBinder(f)
}

func staticServerBinder(server protocol.Server) jsonrpc2_v2.Binder {
	f := func(ctx context.Context, client protocol.ClientCloser) protocol.Server {
		return server
	}
	return NewServerBinder(f)
}

func TestClientLoggingV2(t *testing.T) {
	ctx := context.Background()

	for name, forwarded := range map[string]bool{
		"forwarded":  true,
		"standalone": false,
	} {
		t.Run(name, func(t *testing.T) {
			client := FakeClient{Logs: make(chan string, 10)}
			env := new(TestEnv)
			defer env.Shutdown(t)
			l, _ := env.serve(ctx, t, staticServerBinder(PingServer{}))
			conn := env.dial(ctx, t, l.Dialer(), staticClientBinder(client), forwarded)

			if err := protocol.ServerDispatcherV2(conn).DidOpen(ctx, &protocol.DidOpenTextDocumentParams{}); err != nil {
				t.Errorf("DidOpen: %v", err)
			}
			select {
			case got := <-client.Logs:
				want := "ping"
				matched, err := regexp.MatchString(want, got)
				if err != nil {
					t.Fatal(err)
				}
				if !matched {
					t.Errorf("got log %q, want a log containing %q", got, want)
				}
			case <-time.After(1 * time.Second):
				t.Error("timeout waiting for client log")
			}
		})
	}
}

func TestRequestCancellationV2(t *testing.T) {
	ctx := context.Background()

	for name, forwarded := range map[string]bool{
		"forwarded":  true,
		"standalone": false,
	} {
		t.Run(name, func(t *testing.T) {
			server := WaitableServer{
				Started:   make(chan struct{}),
				Completed: make(chan error),
			}
			env := new(TestEnv)
			defer env.Shutdown(t)
			l, _ := env.serve(ctx, t, staticServerBinder(server))
			client := FakeClient{Logs: make(chan string, 10)}
			conn := env.dial(ctx, t, l.Dialer(), staticClientBinder(client), forwarded)

			sd := protocol.ServerDispatcherV2(conn)
			ctx, cancel := context.WithCancel(ctx)

			result := make(chan error)
			go func() {
				_, err := sd.Hover(ctx, &protocol.HoverParams{})
				result <- err
			}()
			// Wait for the Hover request to start.
			<-server.Started
			cancel()
			if err := <-result; err == nil {
				t.Error("nil error for cancelled Hover(), want non-nil")
			}
			if err := <-server.Completed; err == nil || !strings.Contains(err.Error(), "cancelled hover") {
				t.Errorf("Hover(): unexpected server-side error %v", err)
			}
		})
	}
}
