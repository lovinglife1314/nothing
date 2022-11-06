package internal

import (
	"github.com/spf13/viper"
	"strings"
)

const (
	defaultConfigSecretsFileName = "secrets" // don't include extension
)

func init() {
	viper.SetDefault("FX_ENABLE_EVENT_LOGGER_FLOAT_FIX", false)
	viper.SetDefault("FX_DISABLE_ETCD", false)
	viper.SetDefault("FX_DISABLE_EVENT_LOGGER", false)
	viper.SetDefault("FX_ENABLE_HTTP_RICH_TRACE", false)
	viper.SetDefault("FX_DISABLE_ETCD_SESSION", false)
	viper.SetDefault("FX_ENABLE_GRPC_SERVER_PANIC_RECOVER", false)
	viper.SetDefault("FX_ENABLE_GRPC_SERVER_PANIC_RECOVER_STACKTRACE", true)
	viper.SetDefault("FX_ENABLE_CONFIG_VARIABLES", false)
	viper.SetDefault("FX_CONFIG_VARIABLES_FILES", "")
	viper.SetDefault("FX_ENABLE_EVENT_LOGGER_SHUSHU_IP_FIELD", false)
	viper.SetDefault("FX_ENABLE_GRPC_LOGGER_ADD_UUID", false)
}

func IsETCDEnabled() bool {
	return !viper.GetBool("FX_DISABLE_ETCD")
}

func IsEventLoggerFloatFixEnabled() bool {
	return viper.GetBool("FX_ENABLE_EVENT_LOGGER_FLOAT_FIX")
}

func IsEventLoggerEnabled() bool {
	return !viper.GetBool("FX_DISABLE_EVENT_LOGGER")
}

func IsHTTPRichTraceEnabled() bool {
	return viper.GetBool("FX_ENABLE_HTTP_RICH_TRACE")
}

func IsETCDSessionEnabled() bool {
	return !viper.GetBool("FX_DISABLE_ETCD_SESSION")
}

func IsConfigVariablesEnabled() bool {
	return viper.GetBool("FX_ENABLE_CONFIG_VARIABLES")
}

func GetConfigVariablesFiles() []string {
	filesStr := viper.GetString("FX_CONFIG_VARIABLES_FILES")

	if filesStr == "" {
		filesStr = defaultConfigSecretsFileName
	}

	files := strings.Split(filesStr, ",")
	return files
}

func IsGRPCServerRecoverEnabled() bool {
	return viper.GetBool("FX_ENABLE_GRPC_SERVER_PANIC_RECOVER")
}

func IsGRPCServerRecoverStackTraceEnabled() bool {
	return viper.GetBool("FX_ENABLE_GRPC_SERVER_PANIC_RECOVER_STACKTRACE")
}

func IsEventLoggerSHUSHUIPFieldEnabled() bool {
	return viper.GetBool("FX_ENABLE_EVENT_LOGGER_SHUSHU_IP_FIELD")
}

func IsGrpcLogAddUUID() bool {
	return viper.GetBool("FX_ENABLE_GRPC_LOGGER_ADD_UUID")
}

