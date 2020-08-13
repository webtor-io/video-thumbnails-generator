package services

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	logrusmiddleware "github.com/bakins/logrus-middleware"
	joonix "github.com/joonix/log"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

// Web serves previews for video content
type Web struct {
	host      string
	port      int
	ln        net.Listener
	sourceURL string
	gp        *GeneratorPool
}

const (
	webHostFlag  = "host"
	webPortFlag  = "port"
	webSourceURL = "source-url"
)

// RegisterWebFlags reigisters flags for web server
func RegisterWebFlags(c *cli.App) {
	c.Flags = append(c.Flags, cli.StringFlag{
		Name:  webHostFlag,
		Usage: "listening host",
		Value: "",
	})
	c.Flags = append(c.Flags, cli.StringFlag{
		Name:   webSourceURL,
		Usage:  "source url",
		Value:  "",
		EnvVar: "SOURCE_URL",
	})
	c.Flags = append(c.Flags, cli.IntFlag{
		Name:  webPortFlag,
		Usage: "http listening port",
		Value: 8080,
	})
}

// NewWeb initializes Web
func NewWeb(c *cli.Context, gp *GeneratorPool) *Web {
	return &Web{sourceURL: c.String(webSourceURL), host: c.String(webHostFlag), port: c.Int(webPortFlag), gp: gp}
}

func (s *Web) getInfoHash(r *http.Request) string {
	return r.Header.Get("X-Info-Hash")
}
func (s *Web) getPath(r *http.Request) string {
	return r.Header.Get("X-Path")
}

func (s *Web) getSourceURL(r *http.Request) string {
	if s.sourceURL != "" {
		return s.sourceURL
	}
	return r.Header.Get("X-Source-Url")
}

func (s *Web) getOffset(r *http.Request) (time.Duration, error) {
	offset := r.URL.Query().Get("offset")
	if offset == "" {
		return 5 * time.Minute, nil
	}
	i, err := strconv.Atoi(offset)
	if err != nil {
		return 0, errors.Wrapf(err, "Failed to parse offset")
	}
	return time.Duration(i) * time.Second, nil
}
func (s *Web) getLength(r *http.Request) (time.Duration, error) {
	length := r.URL.Query().Get("length")
	if length == "" {
		return 0 * time.Second, nil
	}
	i, err := strconv.Atoi(length)
	if err != nil {
		return 0, errors.Wrapf(err, "Failed to parse length")
	}
	return time.Duration(i) * time.Second, nil
}

func (s *Web) getReader(r *http.Request) (io.Reader, error) {
	sURL := s.getSourceURL(r)
	parsedURL, err := url.Parse(sURL)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to parse url=%v", sURL)
	}
	sURL = parsedURL.String()
	infoHash := s.getInfoHash(r)
	path := s.getPath(r)
	offset, err := s.getOffset(r)
	if err != nil {
		return nil, errors.Errorf("Failed to get offset")
	}
	length, err := s.getLength(r)
	if err != nil {
		return nil, errors.Errorf("Failed to get length")
	}
	g := s.gp.Get(sURL, offset, length, infoHash, path)
	rr, err := g.Get()
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to get reader")
	}
	return rr, nil
}

// Serve serves web server
func (s *Web) Serve() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "Failed to web listen to tcp connection")
	}
	s.ln = ln
	mux := http.NewServeMux()
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		rr, err := s.getReader(r)
		if err != nil {
			log.WithError(err).Error("Failed to generate thumbnail")
			w.WriteHeader(500)
			return
		}
		_, err = io.Copy(w, rr)
		if err != nil {
			log.WithError(err).Error("Failed to read thumbnail")
			w.WriteHeader(500)
			return
		}
	})
	log.Infof("Serving Web at %v", addr)

	logger := log.New()
	logger.SetFormatter(joonix.NewFormatter())
	l := logrusmiddleware.Middleware{
		Logger: logger,
	}
	return http.Serve(ln, l.Handler(mux, ""))
}

// Close closes web server
func (s *Web) Close() {
	if s.ln != nil {
		s.ln.Close()
	}
}
