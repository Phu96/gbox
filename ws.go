package gbox

import (
	"bufio"
	"bytes"
	"encoding/json"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/gobwas/ws/wsutil"
	"github.com/jensneuse/graphql-go-tools/pkg/graphql"
	"net"
	"net/http"
	"time"
)

type wsMetricsResponseWriter struct {
	requestMetrics
	*caddyhttp.ResponseWriterWrapper
}

func newWebsocketMetricsResponseWriter(w http.ResponseWriter, rm requestMetrics) *wsMetricsResponseWriter {
	return &wsMetricsResponseWriter{
		requestMetrics: rm,
		ResponseWriterWrapper: &caddyhttp.ResponseWriterWrapper{
			ResponseWriter: w,
		},
	}
}

// Hijack connection for collecting subscription metrics.
func (r *wsMetricsResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, w, e := r.ResponseWriterWrapper.Hijack()

	if c != nil {
		c = &wsMetricsConn{
			Conn:           c,
			requestMetrics: r.requestMetrics,
		}
	}

	return c, w, e
}

type wsMetricsConn struct {
	net.Conn
	requestMetrics
	request     *graphql.Request
	subscribeAt time.Time
}

func (c *wsMetricsConn) Read(b []byte) (n int, err error) {
	n, err = c.Conn.Read(b)

	if err != nil {
		if c.request != nil {
			c.addMetricsEndRequest(c.request, time.Since(c.subscribeAt))
			c.request = nil
		}

		return
	}

	buff := bufferPool.Get().(*bytes.Buffer)
	defer bufferPool.Put(buff)
	buff.Reset()
	buff.Write(b[:n])

	r := wsutil.NewServerSideReader(buff)

	if _, e := r.NextFrame(); e != nil {
		return
	}

	decoder := json.NewDecoder(r)
	msg := &struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}{}

	// TODO: implement decompress message via `Sec-WebSocket-Extensions` upgrade header.
	if e := decoder.Decode(msg); e != nil {
		return
	}

	if msg.Type == "subscribe" || msg.Type == "start" {
		request := new(graphql.Request)

		if e := json.Unmarshal(msg.Payload, request); e != nil {
			return
		}

		c.request = request
		c.subscribeAt = time.Now()
		c.addMetricsBeginRequest(request)
	}

	return
}
