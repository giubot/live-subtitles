// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"net"
	"net/http"
)

// Request is what the HTTP layer learned about a request, carried in its
// context so strict handlers (which don't see *http.Request) can use it.
type Request struct {
	// Client is the peer IP address, used for the login rate limit. Proxy
	// headers are ignored: the server is reached directly on the LAN.
	Client string
	// Secure is set for TLS requests (Secure cookie flag).
	Secure bool
	// Admin is set when the request carries valid admin credentials.
	Admin bool
	// Cookie is the admin cookie value, if any (for logout).
	Cookie string
}

type requestKey struct{}

// NewRequest describes r; admin is the result of Service.Authenticated.
func NewRequest(r *http.Request, admin bool) Request {
	client := r.RemoteAddr
	if host, _, err := net.SplitHostPort(client); err == nil {
		client = host
	}
	req := Request{Client: client, Secure: r.TLS != nil, Admin: admin}
	if c, err := r.Cookie(CookieName); err == nil {
		req.Cookie = c.Value
	}
	return req
}

// WithRequest returns ctx carrying req.
func WithRequest(ctx context.Context, req Request) context.Context {
	return context.WithValue(ctx, requestKey{}, req)
}

// RequestFrom returns the Request stored by WithRequest (zero if none).
func RequestFrom(ctx context.Context) Request {
	req, _ := ctx.Value(requestKey{}).(Request)
	return req
}
