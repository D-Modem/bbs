package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/sockets"
)

// metrics writes the Prometheus text format.
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	var b strings.Builder
	put := func(name, kind, help string, v any, labels ...string) {
		if help != "" {
			fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
		}
		l := ""
		if len(labels) == 2 {
			l = fmt.Sprintf(`{%s=%q}`, labels[0], labels[1])
		}
		fmt.Fprintf(&b, "%s%s %v\n", name, l, v)
	}
	nodes := s.B.Nodes()
	byVia := map[string]int{}
	for _, n := range nodes {
		byVia[strings.Fields(n.Via + " ?")[0]]++
	}
	put("bbs_nodes", "gauge", "Nodes in use.", len(nodes))
	vias := make([]string, 0, len(byVia))
	for v := range byVia {
		vias = append(vias, v)
	}
	sort.Strings(vias)
	for i, v := range vias {
		help := ""
		if i == 0 {
			help = "Nodes in use by how the caller came."
		}
		put("bbs_nodes_by_via", "gauge", help, byVia[v], "via", v)
	}
	put("bbs_max_nodes", "gauge", "Nodes there are.", s.B.Conf().MaxNodes)
	if us, err := s.B.Store.Users(); err == nil {
		put("bbs_users", "gauge", "User accounts.", len(us))
	}
	if as, err := s.B.Store.Areas(); err == nil {
		total := 0
		for _, a := range as {
			total += a.Messages
		}
		put("bbs_messages", "gauge", "Messages in the areas.", total)
	}
	st := &s.B.Stats
	put("bbs_logins_total", "counter", "Logins since the start.", st.Logins.Load())
	put("bbs_login_failures_total", "counter", "Failed logins since the start.", st.LoginFailures.Load())
	put("bbs_posts_total", "counter", "Messages posted here since the start.", st.Posts.Load())
	put("bbs_mail_sent_total", "counter", "Mail sent since the start.", st.Mail.Load())
	put("bbs_pages_total", "counter", "Pages of the sysop since the start.", st.Pages.Load())
	put("bbs_chat_lines_total", "counter", "Lines said in chat since the start.", st.ChatLines.Load())
	put("bbs_pages_waiting", "gauge", "Callers paging the sysop now.", len(s.B.Pages()))
	if s.Fido != nil {
		for i, l := range s.Fido.S.Links {
			help := ""
			if i == 0 {
				help = "Files waiting for a FidoNet link."
			}
			put("bbs_fido_outbound_files", "gauge", help, s.Fido.Outbound(l), "link", l.Name)
		}
	}
	w.Write([]byte(b.String()))
}

// serveMetrics serves /metrics on its own address.
func (s *Server) serveMetrics(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", s.metrics)
	ln, err := sockets.Listen("tcp", s.S.Metrics)
	if err != nil {
		s.B.Log.Printf("http: metrics: %v", err)
		return
	}
	hs := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second, ErrorLog: s.B.Log}
	go func() {
		<-ctx.Done()
		hs.Close()
	}()
	s.B.Log.Printf("http: metrics on %s", ln.Addr())
	hs.Serve(ln)
}
