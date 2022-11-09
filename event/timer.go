package event

import (
	"time"
)

// TimerCallback is timer event callback
type TimerCallback func(now time.Time)

// EventTimer is timer data
type EventTimer struct {
	name     string        // timer name
	fireTime time.Time     // next trigger time
	interval time.Duration // interval time
	callback TimerCallback
	repeat   bool // is repeat timer event
	timerID  uint64
}

// Cancel cancel this timer
func (t *EventTimer) Cancel() {
	t.callback = nil
}

// timerHeap a heap for sorted timeout
type timerHeap struct {
	timers []*EventTimer
}

func (h *timerHeap) Len() int {
	return len(h.timers)
}

// Top get first trigger timer
func (h *timerHeap) Top() *EventTimer {
	if h.Len() == 0 {
		return nil
	}
	return h.timers[0]
}

func (h *timerHeap) Less(i, j int) bool {
	t1, t2 := h.timers[i].fireTime, h.timers[j].fireTime
	if t1.Before(t2) {
		return true
	}
	if t1.After(t2) {
		return false
	}
	// t1 == t2, making sure Timer with same deadline is fired according to their add order
	return h.timers[i].timerID < h.timers[j].timerID
}

func (h *timerHeap) Swap(i, j int) {
	tmp := h.timers[i]
	h.timers[i] = h.timers[j]
	h.timers[j] = tmp
}

func (h *timerHeap) Push(x interface{}) {
	h.timers = append(h.timers, x.(*EventTimer))
}

func (h *timerHeap) Pop() (ret interface{}) {
	l := len(h.timers)
	h.timers, ret = h.timers[:l-1], h.timers[l-1]
	return
}
