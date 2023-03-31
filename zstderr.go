package main

import (
	"strings"

	"github.com/rs/zerolog"
)

type ZStderr zerolog.Logger

func (zs ZStderr) Write(p []byte) (n int, err error) {
	log := (*zerolog.Logger)(&zs)
	for _, s := range strings.Split(string(p), "\n") {
		s = strings.TrimSpace(s)
		if len(s) > 0 {
			log.Info().Msg(s)
		}
	}
	return len(p), nil
}
