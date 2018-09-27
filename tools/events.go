package tools

// threaded holds an exit channel to allow threads to break from a select
type threaded struct {
	exit chan struct{}
}

// Event is a type meant to clearly convey that the return type or parameter to
// a function will be supplied to/from an events.Manager
type Event interface{}

// Receiver is a consumer of events, ProcessEvent will be called serially
// as events arrive
type Receiver interface {
	// ProcessEvent delivers an event to the Receiver, if it returns non-nil, the return is the next processed event
	ProcessEvent(e Event) Event
}

// Manager provides a serialized interface for submitting events to
// a Receiver on the other side of the queue
type Manager interface {
	Inject(Event)         // A temporary interface to allow the event manager thread to skip the queue
	Queue() chan<- Event  // Get a write-only reference to the queue, to submit events
	SetReceiver(Receiver) // Set the target to route events to
	Start()               // Starts the Manager thread TODO, these thread management things should probably go away
	Halt()                // Stops the Manager thread
}

// managerImpl is an implementation of Manger
type managerImpl struct {
	threaded
	receiver Receiver
	events   chan Event
}
