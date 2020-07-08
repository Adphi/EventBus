package eventbus

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type EventA struct{}

func (*EventA) Topic() string {
	return "event-A"
}

type EventB struct{}

func (*EventB) Topic() string {
	return "event-B"
}

func TestNew(t *testing.T) {
	bus := New()
	assert.NotNil(t, bus)
}

func TestHasCallback(t *testing.T) {
	bus := New()
	err := bus.SubscribeSync(func(a *EventA) error { return nil })
	require.NoError(t, err)
	assert.False(t, bus.HasCallback(&EventB{}))
	assert.True(t, bus.HasCallback(&EventA{}))
}

func TestSubscribe(t *testing.T) {
	bus := New()
	assert.NoError(t, bus.SubscribeSync(func(*EventB) error { return nil }))
	assert.Error(t, bus.SubscribeSync("topic"))
}

func TestSubscribeOnce(t *testing.T) {
	bus := New()
	assert.NoError(t, bus.SubscribeOnce(func(a *EventA) error { return nil }))
	assert.Error(t, bus.SubscribeOnce(func(a *EventA) {}))
}

func TestSubscribeOnceAndManySubscribe(t *testing.T) {
	bus := New()
	flag := 0
	fn := func(e *EventA) error { flag += 1; return nil }
	assert.NoError(t, bus.SubscribeOnceSync(fn))
	assert.NoError(t, bus.SubscribeSync(fn))
	assert.NoError(t, bus.SubscribeSync(fn))
	assert.NoError(t, bus.Publish(&EventA{}))
	assert.Equal(t, 3, flag)
}

func TestUnsubscribe(t *testing.T) {
	bus := New()
	handler := func(a Event1) error { return nil }
	assert.NoError(t, bus.SubscribeSync(handler))
	assert.NoError(t, bus.Unsubscribe(handler))
	assert.Error(t, bus.Unsubscribe(handler))
}

func TestUnsubscribeMethod(t *testing.T) {
	bus := New()
	val := 0
	h := func(a *EventA) error {
		val++
		return nil
	}
	assert.NoError(t, bus.SubscribeSync(h))
	assert.NoError(t, bus.Publish(&EventA{}))
	assert.NoError(t, bus.Unsubscribe(h))
	assert.Error(t, bus.Unsubscribe(h))
	assert.NoError(t, bus.Publish(&EventA{}))
	bus.WaitAsync()
	assert.Equal(t, 1, val)
}

type A struct {
	val int
	obj interface{}
}

func (a *A) Topic() string {
	return "a"
}

func TestPublish(t *testing.T) {
	bus := New()

	err := bus.SubscribeSync(func(a *A) error {
		if a == nil {
			t.FailNow()
		}
		if a.val != 10 {
			t.Fail()
		}
		if a.obj != nil {
			t.Fail()
		}
		return nil
	})
	assert.NoError(t, err)
	assert.NoError(t, bus.Publish(&A{val: 10}))
	assert.Error(t, bus.Publish(nil))
}

type Event1 struct {
	a int
	out *[]int
	dur string
}
func (Event1) Topic() string {
	return "event1"
}

func TestSubcribeOnceAsync(t *testing.T) {
	results := make([]int, 0)
	bus := New()
	assert.NoError(t, bus.SubscribeOnce(func(e *Event1) error {
		results = append(results, e.a)
		return nil
	}))

	assert.NoError(t, bus.Publish(&Event1{a: 10}))
	assert.NoError(t, bus.Publish(&Event1{a: 10}))

	bus.WaitAsync()

	assert.Len(t, results, 1)

	assert.False(t, bus.HasCallback(&Event1{}))
}

func TestSubscribeAsyncTransactional(t *testing.T) {
	results := make([]int, 0)

	bus := New()
	assert.NoError(t, bus.Subscribe(func(e Event1) error {
		sleep, _ := time.ParseDuration(e.dur)
		time.Sleep(sleep)
		*e.out = append(*e.out, e.a)
		return nil
	}, true))

	assert.NoError(t, bus.Publish(Event1{1, &results, "1s"}))
	assert.NoError(t, bus.Publish(Event1{2, &results, "0s"}))

	bus.WaitAsync()

	require.Len(t, results, 2)
	assert.Equal(t, 1, results[0])
	assert.Equal(t, 2, results[1])
}

