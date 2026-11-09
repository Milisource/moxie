package jd

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewZapFromSlog wraps a *slog.Logger as a *zap.SugaredLogger suitable for
// the underlying jdownloader-go client (which hard-requires zap).
//
// moxie's own logger is slog-based (internal/log); passing its logger here
// keeps JD's debug output in the same sink — including the file-only mode
// used by the TUI, where anything written to stderr would corrupt the
// screen. Log level filtering stays with slog, so
//
//	jd.New(email, pass, jd.WithZapLogger(jd.NewZapFromSlog(log.Logger)))
//
// automatically honors MOXIE_LOG_LEVEL and the project's log level config.
func NewZapFromSlog(sl *slog.Logger) *zap.SugaredLogger {
	if sl == nil {
		return zap.NewNop().Sugar()
	}
	return zap.New(slogCore{logger: sl}).Sugar()
}

// slogCore adapts a *slog.Logger to zapcore.Core. It is intentionally
// small: it forwards messages and the common zap field types, and lets
// slog decide whether a level is enabled.
type slogCore struct {
	logger *slog.Logger
	attrs  []slog.Attr
}

func (c slogCore) Enabled(zapcore.Level) bool { return true } // slog filters

func (c slogCore) With(fields []zapcore.Field) zapcore.Core {
	clone := c
	clone.attrs = append(append([]slog.Attr(nil), c.attrs...), zapFieldsToSlog(fields)...)
	return clone
}

func (c slogCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.logger.Enabled(context.Background(), zapToSlogLevel(ent.Level)) {
		return ce.AddCore(ent, c)
	}
	return ce
}

func (c slogCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	attrs := make([]slog.Attr, 0, len(c.attrs)+len(fields))
	attrs = append(attrs, c.attrs...)
	attrs = append(attrs, zapFieldsToSlog(fields)...)
	c.logger.LogAttrs(context.Background(), zapToSlogLevel(ent.Level), ent.Message, attrs...)
	return nil
}

func (c slogCore) Sync() error { return nil }

func zapToSlogLevel(l zapcore.Level) slog.Level {
	switch {
	case l < zapcore.InfoLevel:
		return slog.LevelDebug
	case l < zapcore.WarnLevel:
		return slog.LevelInfo
	case l < zapcore.ErrorLevel:
		return slog.LevelWarn
	default:
		return slog.LevelError
	}
}

// zapFieldsToSlog converts the zap field types jdownloader-go actually
// emits (mostly strings and ints from its Debugf-style calls); anything
// unusual degrades to slog.Any.
func zapFieldsToSlog(fields []zapcore.Field) []slog.Attr {
	if len(fields) == 0 {
		return nil
	}
	attrs := make([]slog.Attr, 0, len(fields))
	for _, f := range fields {
		if f.Type == zapcore.NamespaceType || f.Type == zapcore.SkipType {
			continue
		}
		attrs = append(attrs, zapFieldToSlog(f))
	}
	return attrs
}

func zapFieldToSlog(f zapcore.Field) slog.Attr {
	switch f.Type {
	case zapcore.StringType:
		return slog.String(f.Key, f.String)
	case zapcore.BoolType:
		return slog.Bool(f.Key, f.Integer == 1)
	case zapcore.Int64Type, zapcore.Int32Type, zapcore.Uint64Type,
		zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type, zapcore.UintptrType:
		return slog.Int64(f.Key, f.Integer)
	case zapcore.Float64Type:
		return slog.Float64(f.Key, math.Float64frombits(uint64(f.Integer)))
	case zapcore.Float32Type:
		return slog.Float64(f.Key, float64(math.Float32frombits(uint32(f.Integer))))
	case zapcore.ByteStringType:
		return slog.String(f.Key, string(f.String))
	case zapcore.DurationType:
		return slog.Duration(f.Key, time.Duration(f.Integer))
	case zapcore.TimeType:
		t := time.Unix(0, f.Integer)
		if loc, ok := f.Interface.(*time.Location); ok && loc != nil {
			t = t.In(loc)
		}
		return slog.Time(f.Key, t)
	case zapcore.ErrorType:
		if err, ok := f.Interface.(error); ok {
			return slog.Any(f.Key, err)
		}
		return slog.String(f.Key, f.String)
	case zapcore.StringerType:
		if s, ok := f.Interface.(fmt.Stringer); ok {
			return slog.String(f.Key, s.String())
		}
		return slog.Any(f.Key, f.Interface)
	case zapcore.ReflectType:
		return slog.Any(f.Key, f.Interface)
	default:
		return slog.Any(f.Key, f.Interface)
	}
}
