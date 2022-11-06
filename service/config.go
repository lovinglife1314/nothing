package service

import "github.com/spf13/pflag"
type serviceConfig struct {
	Name        string
	Description string
	Owner       string
	Roles       []string
	Tags        map[string]string
}

type tlsConfig struct {
	CAFile    string `mapstructure:"ca_file"`
	EnableTLS bool   `mapstructure:"enable_tls"`
}

type sessionConfig struct {
	TTL int
}

type loggingConfig struct {
	Env         string
	Path        string
	MaxSize     uint64 `mapstructure:"max_size"`
	Cron        string
	Level       string
	Stderr      bool // output to stderr at same time
	Stdout      bool // output to stdout at same time
	ErrorMetric bool `mapstructure:"error_metric"`
	Unique      bool
}


func bindFlags(flagset *pflag.FlagSet) {
	flagset.StringSlice("roles", []string{}, "roles to enable")
	flagset.String("config-path", "", "config path to locate config files")
}