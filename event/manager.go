package event

import (
	"container/heap"
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

type EventCallback func(event *Event) (interface{}, error)
type DispatcherMap map[string]func(*Event)

// EventManager is async event handler manager
type EventManager struct {
	Name          string
	dispatchMutex sync.Mutex    // 事件注册锁
	dispatcher    DispatcherMap // 注册的事件分发

	workMutex     sync.Mutex // 事件写入端锁
	workBufferPut []*Event   // 写入的事件列表
	workBufferGet []*Event   // 正在处理的事件列表
	workStartFlag bool       // 开始事件调度标志

	timerMutex  sync.Mutex
	nextTimerID uint64
	timerHeap   timerHeap
	timer       *time.Timer

	logGuard sync.RWMutex
	logger   logger
	logLvl   LogLevel

	idleLoopSleep int
}

// NewEventManager init an instance of EventManager and return
func NewEventManager(opt *EventManagerOptions) *EventManager {
	opt.init()
	mgr := &EventManager{
		Name:          opt.Name,
		workBufferPut: make([]*Event, 0, opt.Capacity),
		workBufferGet: make([]*Event, 0, opt.Capacity),
		dispatcher:    make(map[string]func(*Event)),
		nextTimerID:   1,
		logger:        opt.Logger,
		logLvl:        opt.LogLvl,
		idleLoopSleep: opt.IdleLoopSleep,
	}
	heap.Init(&mgr.timerHeap)
	return mgr
}

// Serve start guard work. an error will be returned if the ctx complete
func (mgr *EventManager) Serve(ctx context.Context) error {
	mgr.workStartFlag = true
	for {
		workCtx, workCancel := context.WithCancel(context.Background())
		go mgr.worker(mgr.cloneDispatcher(), workCancel)

		select {
		case <-workCtx.Done():
			continue
		case <-ctx.Done():
			ev := newExitEvent()
			mgr.pushEvent(ev)
			return fmt.Errorf("whiskey serve err %w", ctx.Err())
		}
	}
}

// the core event loop of this EventManager
func (mgr *EventManager) worker(dispatcher DispatcherMap, cancel context.CancelFunc) {
	idleLoop := 0
	for {
		mgr.swapWorkBuff()
		if len(mgr.workBufferGet) == 0 {
			idleLoop++
			if idleLoop >= mgr.idleLoopSleep {
				idleLoop = 0
				time.Sleep(time.Millisecond)
			} else {
				runtime.Gosched()
			}
			continue
		}
		idleLoop = 0

		for index, event := range mgr.workBufferGet {
			switch event.eventType {
			case EventTypeEvent:
				mgr.handleEvent(dispatcher, event)
			case EventTypeTimer:
				mgr.handleTimer()
			case EventTypeExec:
				mgr.handleExec(event)
			case EventTypeExit:
				return
			case EventTypeRefresh:
				mgr.workMutex.Lock()
				defer mgr.workMutex.Unlock()
				mgr.workBufferPut = append(mgr.workBufferPut, mgr.workBufferGet[index+1:]...)
				mgr.workBufferGet = mgr.workBufferGet[:0]
				cancel()
				return
			}
		}
		mgr.workBufferGet = mgr.workBufferGet[:0]
	}
}

// handleEvent for loop handling a single named event
func (mgr *EventManager) handleEvent(dispatcher DispatcherMap, ev *Event) {
	if cb, ok := dispatcher[ev.Name()]; ok {
		defer func() {

			if r := recover(); r != nil {
				mgr.log(LogLevelError, "async event event:%s recover panic %v stacktrace: %s", ev.Name(), r, string(debug.Stack()))
				_ = ev.Response(nil, fmt.Errorf("%v", r)) // nolint: goerr113
			}
		}()

		cb(ev)
	} else {
		mgr.log(LogLevelWarning, "async event event:%s callback not exists", ev.Name())
		_ = ev.Response(nil, ErrEventNotExists)
	}
}

// handleExec for loop handling a single callback event
func (mgr *EventManager) handleExec(ev *Event) {
	defer func() {
		if r := recover(); r != nil {
			mgr.log(LogLevelError, "async event exec recover panic %v stacktrace: %s", r, string(debug.Stack()))
		}
	}()

	ev.exec()
}

// handleTimer for loop check and exec trigger timer
func (mgr *EventManager) handleTimer() {
	now := time.Now()
	var repeatTimer []*EventTimer
	for {
		if mgr.timerHeap.Len() == 0 {
			break
		}

		top := mgr.timerHeap.Top()
		if top.fireTime.After(now) {
			break
		}

		var t *EventTimer
		var ok bool
		if t, ok = heap.Pop(&mgr.timerHeap).(*EventTimer); !ok {
			continue
		}
		cb := t.callback
		if cb == nil {
			continue
		}
		if !t.repeat {
			t.callback = nil
		} else {
			t.fireTime = t.fireTime.Add(t.interval)
			if t.fireTime.Before(now) {
				t.fireTime = now
			}
			repeatTimer = append(repeatTimer, t)
		}

		// callback
		mgr.onTimer(t, cb, now)
	}

	for _, t := range repeatTimer {
		heap.Push(&mgr.timerHeap, t)
	}
	if mgr.timerHeap.Len() != 0 {
		mgr.fireTimer(nil, mgr.timerHeap.Top())
	}
}

// onTimer handling a single timer event
func (mgr *EventManager) onTimer(t *EventTimer, cb TimerCallback, now time.Time) {
	defer func() {
		if r := recover(); r != nil {
			mgr.log(LogLevelError, "async event timer recover panic %v stacktrace: %s", r, string(debug.Stack()))
		}
	}()

	cb(now)
}

// RegisterEvent register a named event to dispatches.
func (mgr *EventManager) RegisterEvent(name string, cb EventCallback) {
	mgr.RegisterEventRaw(name, func(ev *Event) {
		rsp, err := cb(ev)
		if ev.needResponse {
			_ = ev.Response(rsp, err)
		} else if err != nil {
			mgr.log(LogLevelError, "async event event:%s not captured error result:%s", ev.Name(), err)
		}
	})
}

// RegisterEventRaw register a named event to dispatches.
func (mgr *EventManager) RegisterEventRaw(name string, cb func(*Event)) {
	mgr.dispatchMutex.Lock()
	defer mgr.dispatchMutex.Unlock()
	mgr.dispatcher[name] = cb

	if mgr.workStartFlag {
		mgr.pushEvent(newRefreshEvent())
	}
}

// UnregisterEvent cancel dispatch named event
func (mgr *EventManager) UnregisterEvent(name string) {
	mgr.dispatchMutex.Lock()
	defer mgr.dispatchMutex.Unlock()
	delete(mgr.dispatcher, name)

	if mgr.workStartFlag {
		mgr.pushEvent(newRefreshEvent())
	}
}

func (mgr *EventManager) pushEvent(e *Event) {
	mgr.workMutex.Lock()
	defer mgr.workMutex.Unlock()
	mgr.workBufferPut = append(mgr.workBufferPut, e)
}

// CallEvent add name event to event loop and wait handling result
func (mgr *EventManager) CallEvent(ctx context.Context, name string, msg interface{}) (rsp interface{}, err error) {
	ev := newEvent(name, msg, true)
	mgr.pushEvent(ev)

	for {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		default:
		}

		flag := atomic.LoadUint32(&ev.responseFlag)
		if flag == 0 {
			runtime.Gosched()
			continue
		}

		rsp = ev.responseData
		err = ev.responseErr
		return
	}

}

// SendEvent async add name event to event loop
func (mgr *EventManager) SendEvent(name string, msg interface{}) {
	ev := newEvent(name, msg, false)
	mgr.pushEvent(ev)
}

// Exec async add cb function to event loop
func (mgr *EventManager) Exec(name string, cb func()) {
	ev := newExecEvent(name, cb)
	mgr.pushEvent(ev)
}

func (mgr *EventManager) fireTimer(last *EventTimer, cur *EventTimer) {
	if last != nil && cur != nil && last.fireTime.Before(cur.fireTime) {
		return
	}

	if cur == nil {
		return
	}
	if mgr.timer != nil {
		mgr.timer.Stop()
	}
	now := time.Now()
	mgr.timer = time.AfterFunc(cur.fireTime.Sub(now), func() {
		ev := newTimerEvent()
		mgr.pushEvent(ev)
	})
}

// SetTimeout allows us to run cb function once after the d interval of time
func (mgr *EventManager) SetTimeout(name string, d time.Duration, cb TimerCallback) *EventTimer {
	mgr.timerMutex.Lock()
	defer mgr.timerMutex.Unlock()

	t := &EventTimer{
		name:     name,
		fireTime: time.Now().Add(d),
		interval: d,
		callback: cb,
		repeat:   false,
		timerID:  mgr.nextTimerID,
	}
	mgr.nextTimerID++

	mgr.Exec("whiskey_settimeout", func() {
		last := mgr.timerHeap.Top()
		heap.Push(&mgr.timerHeap, t)
		mgr.fireTimer(last, t)
	})

	return t
}

// SetInterval allows us to run cb function repeatedly, starting after the d interval of time,
//   then repeating continuously at that interval
func (mgr *EventManager) SetInterval(name string, d time.Duration, cb TimerCallback) *EventTimer {
	mgr.timerMutex.Lock()
	defer mgr.timerMutex.Unlock()

	t := &EventTimer{
		name:     name,
		fireTime: time.Now().Add(d),
		interval: d,
		callback: cb,
		repeat:   true,
		timerID:  mgr.nextTimerID,
	}
	mgr.nextTimerID++

	mgr.Exec("whiskey_setinterval", func() {
		last := mgr.timerHeap.Top()
		heap.Push(&mgr.timerHeap, t)

		mgr.fireTimer(last, t)
	})

	return t
}

func (mgr *EventManager) cloneDispatcher() DispatcherMap {
	mgr.dispatchMutex.Lock()
	defer mgr.dispatchMutex.Unlock()
	dispatcher := make(DispatcherMap, len(mgr.dispatcher))
	for k, v := range mgr.dispatcher {
		dispatcher[k] = v
	}
	return dispatcher
}

func (mgr *EventManager) swapWorkBuff() {
	mgr.workMutex.Lock()
	defer mgr.workMutex.Unlock()
	mgr.workBufferGet, mgr.workBufferPut = mgr.workBufferPut, mgr.workBufferGet
}

// log to output log
func (mgr *EventManager) log(lvl LogLevel, format string, args ...interface{}) {
	logger, logLvl := mgr.getLogger()

	if logger == nil {
		return
	}

	if logLvl > lvl {
		return
	}

	// ignore the Output error
	_ = logger.Output(2, fmt.Sprintf(format, args...))
}

// SetLogger assigns the logger to use as well as a level
//
// The logger parameter is an interface that requires the following
// method to be implemented (such as the the stdlib log.Logger):
//
//    Output(calldepth int, s string)
//
func (mgr *EventManager) SetLogger(l logger, lvl LogLevel) {
	mgr.logGuard.Lock()

	mgr.logger = l
	mgr.logLvl = lvl

	mgr.logGuard.Unlock()
}

// getLogger return the log with read locks
func (mgr *EventManager) getLogger() (logger, LogLevel) {
	mgr.logGuard.RLock()
	defer mgr.logGuard.RUnlock()

	return mgr.logger, mgr.logLvl
}
