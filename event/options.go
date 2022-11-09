package event

import (
	"log"
	"os"
)

const (
	defaultCapacity = 1024
)

// EventManagerOptions used as config for EventManager
type EventManagerOptions struct {
	Name          string
	Capacity      uint64   // 内部队列的容量
	Logger        logger   // 从外面传递日志进来
	LogLvl        LogLevel // log的级别，默认Debug
	MetricsSuffix string   // 指定指标名后缀 默认空
	IdleLoopSleep int // 连续多少次空闲时sleep 1毫秒 默认10
}

// init add some default values for EventManagerOptions
// logLvl and strict does not need to care
func (opt *EventManagerOptions) init() {
	if opt.Capacity <= 0 {
		opt.Capacity = defaultCapacity
	}
	if opt.Logger == nil {
		opt.Logger = log.New(os.Stdout, "", log.Flags())
	}
	if opt.IdleLoopSleep == 0 {
		opt.IdleLoopSleep = 10
	}
}
