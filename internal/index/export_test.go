package index

import "sync"

// ResetWorkerIDForTest clears the cached process worker identity so a
// subsequent WorkerID() call regenerates a fresh value. The helper is
// only visible to tests in this package's _test files; production
// code never calls it.
func ResetWorkerIDForTest() {
	workerIDOnce = sync.Once{}
	workerIDValue = ""
}
