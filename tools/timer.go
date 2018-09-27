package tools

import (
	"fmt"
	"github.com/DSiSc/craft/log"
	"time"
)

// ------------------------------------------------------------
//
// Event Timer
//
// ------------------------------------------------------------

// Timer is an interface for managing time driven events
// the special contract Timer gives which a traditional golang
// timer does not, is that if the event thread calls stop, or reset
// then even if the timer has already fired, the event will not be
// delivered to the event queue
type Timer interface {
	SoftReset(duration time.Duration, event Event) // start a new countdown, only if one is not already started
	Reset(duration time.Duration, event Event)     // start a new countdown, clear any pending events
	Stop()                                         // stop the countdown, clear any pending events
	Halt()                                         // Stops the Timer thread
}

// timerStart is used to deliver the start request to the eventTimer thread
type timerStart struct {
	hard     bool          // Whether to reset the timer if it is running
	event    Event         // What event to push onto the event queue
	duration time.Duration // How long to wait before sending the event
}

// timerImpl is an implementation of Timer
type timerImpl struct {
	threaded                   // Gives us the exit chan
	timerChan <-chan time.Time // When non-nil, counts down to preparing to do the event
	startChan chan *timerStart // Channel to deliver the timer start events to the service go routine
	stopChan  chan struct{}    // Channel to deliver the timer stop events to the service go routine
	manager   Manager          // The event manager to deliver the event to after timer expiration
}

// halt tells the threaded object's thread to exit
func (t *threaded) Halt() {
	select {
	case <-t.exit:
		log.Info(fmt.Sprintf("Attempted to halt a threaded object twice"))
	default:
		close(t.exit)
	}
}

// newTimer creates a new instance of timerImpl
func newTimerImpl(manager Manager) Timer {
	et := &timerImpl{
		startChan: make(chan *timerStart),
		stopChan:  make(chan struct{}),
		threaded:  threaded{make(chan struct{})},
		manager:   manager,
	}
	go et.loop()
	return et
}

// softReset tells the timer to start a new countdown, only if it is not currently counting down
// this will not clear any pending events
func (et *timerImpl) SoftReset(timeout time.Duration, event Event) {
	et.startChan <- &timerStart{
		duration: timeout,
		event:    event,
		hard:     false,
	}
}

// reset tells the timer to start counting down from a new timeout, this also clears any pending events
func (et *timerImpl) Reset(timeout time.Duration, event Event) {
	et.startChan <- &timerStart{
		duration: timeout,
		event:    event,
		hard:     true,
	}
}

// stop tells the timer to stop, and not to deliver any pending events
func (et *timerImpl) Stop() {
	et.stopChan <- struct{}{}
}

// loop is where the timer thread lives, looping
func (et *timerImpl) loop() {

}
