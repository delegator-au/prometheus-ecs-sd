package logger

import (
	"github.com/rs/zerolog"
	"os"
)

var (
	Log     = zerolog.Logger{}
)

func init() {
	var cluster string
	if v, ok := os.LookupEnv("ECS_CLUSTER"); ok {
		cluster = v
	}

	output := zerolog.ConsoleWriter{Out: os.Stdout, NoColor: true}
	Log = zerolog.New(output).Hook(SeverityHook{}).With().Str("cluster", cluster).Timestamp().Logger()
	Log.Level(zerolog.InfoLevel)
}

type SeverityHook struct{}

func (h SeverityHook) Run(e *zerolog.Event, level zerolog.Level, msg string) {
	if level != zerolog.NoLevel {
		e.Str("severity", level.String())
	}
}
