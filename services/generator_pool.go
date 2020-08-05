package services

import (
	"fmt"
	"sync"
	"time"
)

const (
	generatorTTL = 60 * 60
)

// GeneratorPool ensures that only one specific preview is generating at time
type GeneratorPool struct {
	s3     *S3Storage
	sm     sync.Map
	timers sync.Map
	expire time.Duration
}

// NewGeneratorPool initializes GeneratorPool
func NewGeneratorPool(s3 *S3Storage) *GeneratorPool {
	return &GeneratorPool{s3: s3, expire: time.Duration(generatorTTL) * time.Second}
}

// Get gets Generator
func (s *GeneratorPool) Get(sourceURL string, offset time.Duration, format string, width int, length time.Duration, infoHash string, path string) *Generator {
	key := fmt.Sprintf("%v%v%v%v%v%v", offset, format, width, length, infoHash, path)
	v, _ := s.sm.LoadOrStore(key, NewGenerator(s.s3, sourceURL, offset, format, width, length, infoHash, path))
	t, tLoaded := s.timers.LoadOrStore(key, time.NewTimer(s.expire))
	timer := t.(*time.Timer)
	if !tLoaded {
		if _, err := v.(*Generator).Get(); err != nil {
			s.sm.Delete(key)
			s.timers.Delete(key)
		} else {
			go func() {
				<-timer.C
				s.sm.Delete(key)
				s.timers.Delete(key)
			}()
		}
	} else {
		timer.Reset(s.expire)
	}
	return v.(*Generator)
}
