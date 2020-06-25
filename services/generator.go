package services

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/pkg/errors"
)

// Generator generates previews for video content
type Generator struct {
	sourceURL string
	format    string
	offset    time.Duration
	length    time.Duration
	infoHash  string
	path      string
	width     int
	inited    bool
	b         []byte
	err       error
	mux       sync.Mutex
	s3        *S3Storage
}

// NewGenerator initializes new Generator instance
func NewGenerator(s3 *S3Storage, sourceURL string, offset time.Duration, format string, width int, length time.Duration, infoHash string, path string) *Generator {
	return &Generator{s3: s3, sourceURL: sourceURL, offset: offset, format: format, width: width,
		length: length, infoHash: infoHash, path: path, inited: false}
}

// Using following links:
// https://superuser.com/questions/538112/meaningful-thumbnails-for-a-video-using-ffmpeg
// https://stackoverflow.com/questions/52303867/how-do-i-set-ffmpeg-pipe-output
// https://www.bogotobogo.com/FFMpeg/ffmpeg_image_scaling_jpeg.php
func (s *Generator) getParams() ([]string, error) {
	var codec, format string
	switch s.format {
	case "webp":
		codec = "libwebp"
		format = "image2"
	default:
		return nil, errors.Errorf("Unsupported format type %v", s.format)
	}
	params := []string{
		"-ss", fmt.Sprintf("%d", int(s.offset.Seconds())),
		"-i", s.sourceURL,
		"-frames:v", "1",
		"-c:v", codec,
		"-f", format,
	}
	if s.length == 0 {
		params = append(params, "-frames:v", "1")
	} else {
		params = append(params, "-t", strconv.Itoa(int(s.length.Seconds())))
	}
	vf := []string{"select=gt(scene\\,0.5)"}
	if s.width != 0 {
		vf = append(vf, fmt.Sprintf("scale=%d:-1", s.width))
	}
	params = append(params, "-vf", strings.Join(vf, ","))
	params = append(params, "-")
	logrus.Infof("FFmpeg params %v", params)
	return params, nil
}

func (s *Generator) getKey() string {
	name := fmt.Sprintf("%v-%v-%v.%v", s.width, int(s.offset.Seconds()), int(s.length.Seconds()), s.format)
	path := ""
	if s.infoHash != "" && s.path != "" {
		path = s.infoHash + "/" + s.path
	} else {
		h := md5.New()
		io.WriteString(h, strings.Split(s.sourceURL, "?")[0])
		path = fmt.Sprintf("%x", h.Sum(nil))
	}
	return path + "/" + name
}

func (s *Generator) get() ([]byte, error) {
	key := s.getKey()
	p, err := s.s3.GetPreview(key)
	if err != nil && err != ErrNoPreview {
		return nil, errors.Wrap(err, "Failed to fetch preview")
	}
	if p != nil {
		b, err := ioutil.ReadAll(p)
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to read from S3 key=%v", b)
		}
		p.Close()
		return b, nil
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, errors.Wrap(err, "Failed to find ffmpeg")
	}
	params, err := s.getParams()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get ffmpeg params")
	}

	cmd := exec.Command(ffmpeg, params...)

	var stdoutBuf bytes.Buffer

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get stdout")
	}

	cmd.Stderr = os.Stderr

	err = cmd.Start()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to start ffmpeg")
	}
	done := make(chan error)
	go func() {
		_, err := io.Copy(&stdoutBuf, stdout)
		done <- err
	}()
	err = <-done
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get image")
	}
	b := stdoutBuf.Bytes()
	err = s.s3.PutPreview(key, b)
	if err != nil {
		logrus.Warnf("Failed to put image to S3 key=%v", key)
	}
	return b, nil
}

// Get gets preview
func (s *Generator) Get() (io.Reader, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	if s.inited {
		return bytes.NewReader(s.b), s.err
	}
	s.b, s.err = s.get()
	s.inited = true
	return bytes.NewReader(s.b), s.err
}
