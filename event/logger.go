package event

type logger interface {
	Output(calldepth int, s string) error
}

// Log levels
const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarning
	LogLevelError
)

// LogLevel specifies the severity of a given log message
type LogLevel int

// String returns the string form for a given LogLevel
func (lvl LogLevel) String() string {
	switch lvl {
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarning:
		return "WARNING"
	case LogLevelError:
		return "ERROR"
	case LogLevelDebug:
		return "DEBUG"
	}

	return "DEBUG"
}
