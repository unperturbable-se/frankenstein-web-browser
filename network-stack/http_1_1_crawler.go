package main

import (
	"bufio"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	connIdleTimeout = 30 * time.Second
	poolWaitTimeout = 60 * time.Second
	responseTimeout = 60 * time.Second
	maxConnsPerHost = 6
	maxRedirects    = 5
	maxPages        = 200
)

// Capture tag name; skip <a>
var linkRe = regexp.MustCompile(`(?is)<([a-z][a-z0-9]*)\b[^>]*?\b(?:href|src)\s*=\s*["']([^"']+)["']`)


func normalizeURL(s string) string {
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Hostname())
	u.Fragment = ""
	u.Path = path.Clean(u.Path)
	if u.Path == "." {
		u.Path = "/"
	}
	u.RawQuery = u.Query().Encode()
	return u.String()
}

func absoluteURL(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	if r.Scheme != "" && r.Scheme != "http" && r.Scheme != "https" {
		return ""
	}
	abs := b.ResolveReference(r)
	abs.Fragment = ""
	return abs.String()
}

func extractURLs(body string) []string {
	var out []string
	for _, m := range linkRe.FindAllStringSubmatch(body, -1) {
		if strings.ToLower(m[1]) == "a" {
			continue // navigation, not a subresource
		}
		ref := html.UnescapeString(strings.TrimSpace(m[2]))
		if isFetchableRef(ref) {
			out = append(out, ref)
		}
	}
	return out
}

func isFetchableRef(ref string) bool {
	if ref == "" {
		return false
	}
	for _, p := range []string{"#", "data:", "javascript:", "mailto:", "tel:"} {
		if strings.HasPrefix(ref, p) {
			return false
		}
	}
	return true
}

type wireRequest struct {
	raw  string
	resp chan wireResponse
}

type wireResponse struct {
	body        string
	contentType string
	status      int
	location    string
	ok          bool
}

func isRedirect(code int) bool {
	switch code {
	case 301, 302, 303, 307, 308:
		return true
	}
	return false
}

func readResponse(br *bufio.Reader, conn net.Conn, raw string) (wireResponse, bool) {
	conn.SetDeadline(time.Now().Add(connIdleTimeout))
	if _, err := io.Copy(conn, strings.NewReader(raw)); err != nil {
		return wireResponse{}, false
	}
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		return wireResponse{}, false
	}
	defer resp.Body.Close()
	body, rerr := io.ReadAll(resp.Body)
	wr := wireResponse{
		body:        string(body),
		contentType: resp.Header.Get("Content-Type"),
		status:      resp.StatusCode,
		location:    resp.Header.Get("Location"),
		ok:          rerr == nil,
	}
	return wr, rerr == nil && !resp.Close
}

type connOwner struct {
	conn net.Conn
	reqs chan *wireRequest
	done chan struct{}
	once sync.Once
}

func newConnOwner(conn net.Conn) *connOwner {
	o := &connOwner{conn: conn, reqs: make(chan *wireRequest), done: make(chan struct{})}
	go o.serveLoop()
	return o
}

func (o *connOwner) isDead() bool {
	select {
	case <-o.done:
		return true
	default:
		return false
	}
}

func (o *connOwner) shutdown() {
	o.once.Do(func() { close(o.done); o.conn.Close() })
}

func (o *connOwner) serveLoop() {
	defer o.shutdown()
	br := bufio.NewReader(o.conn)
	for {
		req := o.awaitRequest()
		if req == nil {
			return
		}
		resp, keep := readResponse(br, o.conn, req.raw)
		if !keep {
			o.shutdown()
		}
		req.resp <- resp
		if !keep {
			return
		}
	}
}

func (o *connOwner) awaitRequest() *wireRequest {
	select {
	case r := <-o.reqs:
		return r
	case <-o.done:
		return nil
	}
}

func (o *connOwner) exchange(raw string) (wireResponse, bool) {
	req := &wireRequest{raw: raw, resp: make(chan wireResponse, 1)}
	select {
	case o.reqs <- req:
	case <-o.done:
		return wireResponse{}, false
	}
	select {
	case r := <-req.resp:
		return r, r.ok
	case <-time.After(responseTimeout):
		return wireResponse{}, false
	}
}

func dialHost(host string) (net.Conn, error) {
	ip := dnsResolver(host)
	if ip == "" {
		return nil, fmt.Errorf("dns: %s", host)
	}
	ok, conn := createTCPConnection(ip, "443")
	if !ok {
		return nil, fmt.Errorf("tcp: %s", host)
	}
	return handshakeTLS(conn, host)
}

func dialURL(u string) (net.Conn, error) {
	_, h, _, _ := parseURL(u)
	return dialHost(h)
}

func handshakeTLS(conn net.Conn, host string) (net.Conn, error) {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	ok, tlsConn := startTLSHandshake(conn, host)
	if !ok {
		conn.Close()
		return nil, fmt.Errorf("tls: %s", host)
	}
	return tlsConn, nil
}

func oneShotFetch(u string) (wireResponse, bool) {
	conn, err := dialURL(u)
	if err != nil {
		return wireResponse{}, false
	}
	defer conn.Close()
	return readResponse(bufio.NewReader(conn), conn, httpGET(u, ""))
}

type hostPool struct {
	mu     sync.Mutex
	idle   []*connOwner
	sem    chan struct{}
	host   string
	closed bool
}

func newHostPool(host string, max int) *hostPool {
	return &hostPool{host: host, sem: make(chan struct{}, max)}
}

func (p *hostPool) Close() {
	p.mu.Lock()
	p.closed = true
	for _, o := range p.idle {
		o.shutdown()
	}
	p.idle = nil
	p.mu.Unlock()
}

func (p *hostPool) fetch(raw string) (wireResponse, bool) {
	for i := 0; i < 2; i++ {
		o := p.acquireConn()
		if o == nil {
			return wireResponse{}, false
		}
		r, ok := o.exchange(raw)
		p.releaseConn(o)
		if ok || r.status != 0 {
			return r, ok
		}
	}
	return wireResponse{}, false
}

func (p *hostPool) acquireConn() *connOwner {
	select {
	case p.sem <- struct{}{}:
	case <-time.After(poolWaitTimeout):
		return nil
	}
	if o := p.popIdleConn(); o != nil {
		return o
	}
	return p.openConn()
}

func (p *hostPool) popIdleConn() *connOwner {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.idle) > 0 {
		o := p.idle[len(p.idle)-1]
		p.idle = p.idle[:len(p.idle)-1]
		if !o.isDead() {
			return o
		}
	}
	return nil
}

func (p *hostPool) openConn() *connOwner {
	conn, err := dialHost(p.host)
	if err != nil {
		<-p.sem
		return nil
	}
	return newConnOwner(conn)
}

func (p *hostPool) releaseConn(o *connOwner) {
	p.mu.Lock()
	closed := p.closed || o.isDead()
	if !closed {
		p.idle = append(p.idle, o)
	}
	p.mu.Unlock()
	if closed {
		o.shutdown()
	}
	<-p.sem
}

type crawler struct {
	mu     sync.Mutex
	seen   map[string]bool
	bodies []string
	wg     sync.WaitGroup
	pool   *hostPool
	host   string
}

func newCrawler(root, host string) *crawler {
	return &crawler{
		seen: map[string]bool{root: true},
		pool: newHostPool(host, maxConnsPerHost),
		host: host,
	}
}

func (c *crawler) crawlOne(u string) {
	defer c.wg.Done()
	res, ok := c.pool.fetch(httpGET(u, ""))
	if !ok && res.status == 0 {
		fmt.Printf("POOL-FAIL %s\n", u)
		return
	}
	if !ok {
		fmt.Printf("PARTIAL %d bytes %s\n", len(res.body), u)
	}
	c.process(u, res, 0)
}

func (c *crawler) followRedirect(u string, hops int) {
	if hops >= maxRedirects {
		return
	}
	res, ok := oneShotFetch(u)
	if !ok && res.status == 0 {
		return
	}
	c.process(u, res, hops)
}

func (c *crawler) process(u string, res wireResponse, hops int) {
	if isRedirect(res.status) && res.location != "" {
		if next := normalizeURL(absoluteURL(u, res.location)); next != "" && c.claimRedirect(next) {
			c.followRedirect(next, hops+1)
		}
		return
	}
	if res.status != http.StatusOK {
		fmt.Printf("skip %d %s\n", res.status, u)
		return
	}
	if res.body == "" {
		fmt.Printf("skip empty %s\n", u)
		return
	}
	c.appendBody(res.body)
	if strings.Contains(res.contentType, "text/html") {
		c.spawnCrawlers(c.discoverLinks(res.body, u))
	}
}

func (c *crawler) claimRedirect(link string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen[link] {
		return false
	}
	c.seen[link] = true
	return true
}

func (c *crawler) appendBody(body string) {
	c.mu.Lock()
	c.bodies = append(c.bodies, body)
	c.mu.Unlock()
}

func (c *crawler) discoverLinks(body, base string) []string {
	var out []string
	for _, ref := range extractURLs(body) {
		if link := normalizeURL(absoluteURL(base, ref)); link != "" {
			out = append(out, link)
		}
	}
	return out
}

func (c *crawler) spawnCrawlers(links []string) {
	for _, link := range links {
		if c.claimIfNew(link) {
			c.wg.Add(1)
			go c.crawlOne(link)
		}
	}
}

func (c *crawler) claimIfNew(link string) bool {
	sc, h, _, _ := parseURL(link)
	if sc != "https" || h != c.host {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen[link] || len(c.seen) >= maxPages {
		return false
	}
	c.seen[link] = true
	return true
}

func http1_1(root string) []string {
	root = normalizeURL(root)
	_, host, _, _ := parseURL(root)
	c := newCrawler(root, host)
	defer c.pool.Close()
	c.wg.Add(1)
	go c.crawlOne(root)
	c.wg.Wait()
	return c.bodies
}