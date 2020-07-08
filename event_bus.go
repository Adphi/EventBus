package eventbus

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

var ErrInvalidHandler = errors.New("handler be func(e T extends Event) error")

type Event interface {
	Topic() string
}

// BusSubscriber defines subscription-related bus behavior
type BusSubscriber interface {
	SubscribeSync(fn interface{}) error
	Subscribe(fn interface{}, transactional ...bool) error
	SubscribeOnceSync(fn interface{}) error
	SubscribeOnce(fn interface{}) error
	Unsubscribe(handler interface{}) error
}

// BusPublisher defines publishing-related bus behavior
type BusPublisher interface {
	Publish(e Event) error
}

// BusController defines bus control behavior (checking handler's presence, synchronization)
type BusController interface {
	HasCallback(e Event) bool
	WaitAsync()
}

// Bus englobes global (subscribe, publish, control) bus behavior
type Bus interface {
	BusController
	BusSubscriber
	BusPublisher
}

// EventBus - box for handlers and callbacks.
type EventBus struct {
	handlers map[string][]*eventHandler
	lock     sync.Mutex // a lock for the map
	wg       sync.WaitGroup
}

type eventHandler struct {
	callBack      reflect.Value
	flagOnce      bool
	async         bool
	transactional bool
	sync.Mutex    // lock for an event handler - useful for running async callbacks serially
}

// New returns new EventBus with empty handlers.
func New() Bus {
	b := &EventBus{
		make(map[string][]*eventHandler),
		sync.Mutex{},
		sync.WaitGroup{},
	}
	return Bus(b)
}

// doSubscribe handles the subscription logic and is utilized by the public Subscribe functions
func (bus *EventBus) doSubscribe(fn interface{}, handler *eventHandler) error {
	bus.lock.Lock()
	defer bus.lock.Unlock()
	topic, err := handlerTopic(fn)
	if err != nil {
		return err
	}
	bus.handlers[topic] = append(bus.handlers[topic], handler)
	return nil
}

// SubscribeSync subscribes to an Event type with a synchronous callback.
// Returns error if `fn` is not func(e T extends Event) error.
func (bus *EventBus) SubscribeSync(fn interface{}) error {
	return bus.doSubscribe(fn, &eventHandler{
		reflect.ValueOf(fn), false, false, false, sync.Mutex{},
	})
}

// Subscribe subscribes to an Event type with an asynchronous callback
// Transactional determines whether subsequent callbacks for an Event type are
// run serially (true) or concurrently (false)
// Returns error if `fn` is not func(e T extends Event) error.
func (bus *EventBus) Subscribe(fn interface{}, transactional ...bool) error {
	var t bool
	if len(transactional) > 0 {
		t = transactional[0]
	}
	return bus.doSubscribe(fn, &eventHandler{
		reflect.ValueOf(fn), false, true, t, sync.Mutex{},
	})
}

// SubscribeOnceSync subscribes to an Event type once with a synchronous callback. Handler will be removed after executing.
// Returns error if `fn` is not func(e T extends Event) error.
func (bus *EventBus) SubscribeOnceSync(fn interface{}) error {
	return bus.doSubscribe(fn, &eventHandler{
		reflect.ValueOf(fn), true, false, false, sync.Mutex{},
	})
}

// SubscribeOnce subscribes to an Event type once.
// Handler will be removed after executing.
// Returns error if `fn` is not func(e T extends Event) error.
func (bus *EventBus) SubscribeOnce(fn interface{}) error {
	return bus.doSubscribe(fn, &eventHandler{
		reflect.ValueOf(fn), true, true, false, sync.Mutex{},
	})
}

// HasCallback returns true if exists any callback subscribed to the topic.
func (bus *EventBus) HasCallback(e Event) bool {
	bus.lock.Lock()
	defer bus.lock.Unlock()
	_, ok := bus.handlers[e.Topic()]
	if ok {
		return len(bus.handlers[e.Topic()]) > 0
	}
	return false
}

// Unsubscribe removes callback defined for an Event type.
// Returns error if there are no callbacks subscribed to the topic.
func (bus *EventBus) Unsubscribe(handler interface{}) error {
	bus.lock.Lock()
	defer bus.lock.Unlock()
	topic, err := handlerTopic(handler)
	if err != nil {
		return err
	}
	if _, ok := bus.handlers[topic]; ok && len(bus.handlers[topic]) > 0 {
		bus.removeHandler(topic, bus.findHandlerIdx(topic, reflect.ValueOf(handler)))
		return nil
	}
	return fmt.Errorf("topic %s doesn't exist", topic)
}

// Publish executes callback defined for an Event type. Any additional argument will be transferred to the callback.
func (bus *EventBus) Publish(e Event) error {
	if e == nil {
		return errors.New("event cannot be nil")
	}
	bus.lock.Lock() // will unlock if handler is not found or always after setUpPublish
	defer bus.lock.Unlock()
	topic := e.Topic()
	if handlers, ok := bus.handlers[topic]; ok && 0 < len(handlers) {
		// Handlers slice may be changed by removeHandler and Unsubscribe during iteration,
		// so make a copy and iterate the copied slice.
		copyHandlers := make([]*eventHandler, len(handlers))
		copy(copyHandlers, handlers)
		for i, handler := range copyHandlers {
			if handler.flagOnce {
				bus.removeHandler(topic, i)
			}
			if handler.async {
				bus.wg.Add(1)
				if handler.transactional {
					bus.lock.Unlock()
					handler.Lock()
					bus.lock.Lock()
				}
				go bus.doPublishAsync(handler, e)
			} else if err := bus.doPublish(handler, e); err != nil {
				return err
			}
		}
	}
	return nil
}

func (bus *EventBus) doPublish(handler *eventHandler, e Event) error {
	// passedArguments := bus.setUpPublish(handler, topic, e)
	resultValues := handler.callBack.Call([]reflect.Value{reflect.ValueOf(e)})
	if len(resultValues) > 0 && !resultValues[0].IsNil() {
		return resultValues[0].Interface().(error)
	}
	return nil
}

func (bus *EventBus) doPublishAsync(handler *eventHandler, e Event) error {
	defer bus.wg.Done()
	if handler.transactional {
		defer handler.Unlock()
	}
	return bus.doPublish(handler, e)
}

func (bus *EventBus) removeHandler(topic string, idx int) {
	if _, ok := bus.handlers[topic]; !ok {
		return
	}
	l := len(bus.handlers[topic])

	if !(0 <= idx && idx < l) {
		return
	}

	copy(bus.handlers[topic][idx:], bus.handlers[topic][idx+1:])
	bus.handlers[topic][l-1] = nil // or the zero value of T
	bus.handlers[topic] = bus.handlers[topic][:l-1]
}

func (bus *EventBus) findHandlerIdx(topic string, callback reflect.Value) int {
	if _, ok := bus.handlers[topic]; ok {
		for idx, handler := range bus.handlers[topic] {
			if handler.callBack.Type() == callback.Type() &&
				handler.callBack.Pointer() == callback.Pointer() {
				return idx
			}
		}
	}
	return -1
}

func handlerTopic(fn interface{}) (topic string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("cannot handle event type: %v", r)
		}
	}()
	t := reflect.TypeOf(fn)
	if t.Kind() != reflect.Func {
		return "", ErrInvalidHandler
	}
	if t.NumIn() != 1 {
		return "", ErrInvalidHandler
	}
	if t.NumOut() != 1 {
		return "", ErrInvalidHandler
	}
	if t.Out(0).Name() != "error" {
		return "", ErrInvalidHandler
	}
	in := t.In(0)
	// Check if type implement Event as pointer receiver
	if in.Kind() == reflect.Ptr {
		_, ok := reflect.Zero(in.Elem()).Interface().(Event)
		if ok {
			in = in.Elem()
		}
	}
	e, ok := reflect.Zero(in).Interface().(Event)
	if !ok {
		return "", ErrInvalidHandler
	}
	if !reflect.ValueOf(e).IsValid() {
		return "", fmt.Errorf("%v and T must be a pointer receiver", ErrInvalidHandler)
	}
	return e.Topic(), nil
}

// WaitAsync waits for all async callbacks to complete
func (bus *EventBus) WaitAsync() {
	bus.wg.Wait()
}
