package event

import (
	"sync/atomic"
)

// EventType
const (
	// EventTypeEvent is a named event
	EventTypeEvent = iota
	// EventTypeExec is a callback event
	EventTypeExec
	// EventTypeTimer is a timer check event
	EventTypeTimer
	// EventTypeExit is a system exit event
	EventTypeExit
	// EventTypeRefresh is a change dispatcher control event
	EventTypeRefresh
)

// EventType defines Event Type
type EventType int

type Event struct {
	eventType EventType // type: EventTypeEvent/EventTypeExec/EventTypeTimer/EventTypeExit

	// EventTypeEvent
	name         string      // named event name
	data         interface{} // named event data
	needResponse bool        // named event response
	responseData interface{}
	responseErr  error
	responseFlag uint32

	// EventTypeExec
	exec func() // exec event callback
}

// newEvent create a Event instance use EventTypeEvent type
func newEvent(name string, data interface{}, needResponse bool) *Event {
	ev := &Event{
		eventType: EventTypeEvent,
		name:      name,
		data:      data,
	}
	if needResponse {
		ev.needResponse = needResponse
		ev.responseFlag = 0
	}
	return ev
}

// newTimerEvent create a Event instance use EventTypeTimer type
func newTimerEvent() *Event {
	return &Event{
		eventType: EventTypeTimer,
	}
}

// newTimerEvent create a Event instance use EventTypeExec type
func newExecEvent(name string, cb func()) *Event {
	return &Event{
		eventType: EventTypeExec,
		name:      name,
		exec:      cb,
	}
}

// newTimerEvent create a Event instance use EventTypeExit type
func newExitEvent() *Event {
	return &Event{
		eventType: EventTypeExit,
	}
}

// newRefreshEvent create a Event instance use EventTypeRefresh type
func newRefreshEvent() *Event {
	return &Event{
		eventType: EventTypeRefresh,
	}
}

// Name return EventTypeEvent event name
func (e Event) Name() string {
	return e.name
}

// Data return EventTypeEvent event data
func (e *Event) Data() interface{} {
	return e.data
}

// Response notify CallEvent result. an error will be returned if the response is disposed
func (e *Event) Response(result interface{}, err error) error {
	if !e.needResponse {
		return ErrResponseNeedCall
	}
	e.responseData = result
	e.responseErr = err
	atomic.CompareAndSwapUint32(&e.responseFlag, 0, 1)
	return nil
}
