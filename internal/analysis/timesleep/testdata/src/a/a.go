package a

import "time"

// time.Sleep in non-test files should not be flagged.
func helper() {
	time.Sleep(time.Second)
}
