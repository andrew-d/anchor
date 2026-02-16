package a

import (
	"testing"
	"testing/synctest"
	"time"
)

func TestDirect(t *testing.T) {
	time.Sleep(time.Second) // want `time\.Sleep in test outside synctest bubble`
}

func TestInsideSynctest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		time.Sleep(time.Second) // OK: inside synctest bubble
	})
}

func TestInsideSynctestGoroutine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		go func() {
			time.Sleep(time.Second) // OK: inside synctest bubble
		}()
	})
}

func TestMixed(t *testing.T) {
	time.Sleep(time.Second) // want `time\.Sleep in test outside synctest bubble`
	synctest.Test(t, func(t *testing.T) {
		time.Sleep(time.Second) // OK: inside synctest bubble
	})
}

func testHelper() {
	time.Sleep(time.Second) // want `time\.Sleep in test outside synctest bubble`
}
