// Package speech is the screen-reader output.
//
// It uses Prism (prism.dll: NVDA, JAWS, ZDSR and other screen readers, SAPI fallback). The best
// backend is picked again for each line, so starting or closing a screen reader while the launcher
// runs switches output to it. If prism.dll is missing the launcher still works; the window's controls
// stay readable by the screen reader, only the spoken progress updates are lost.
package speech

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

const (
	prismOK                 = 0
	prismAlreadyInitialized = 15
)

// prismConfig mirrors PrismConfig (56 bytes). prism_config_init returns version 4 and zeros.
type prismConfig struct {
	version            uint8
	_                  [7]byte
	registry           uintptr
	availabilityCB     uintptr
	availabilityUser   uintptr
	pollIntervalMs     uint32
	debounceSamples    uint32
	backoffMaxMs       uint32
	autoPowerManage    bool
	_                  [3]byte
	availabilityBaseCB uintptr
}

var (
	mu      sync.Mutex
	history []string
	ctx     uintptr

	acquireBest, backendInit, backendOutput, backendFree *syscall.Proc
)

func init() {
	if err := load(); err != nil {
		log.Printf("Prism unavailable, speech disabled: %v", err)
	}
}

// load finds prism.dll next to the exe, or in its _internal folder.
func load() error {
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	var dll *syscall.DLL
	var err error
	for _, p := range []string{filepath.Join(dir, "prism.dll"), filepath.Join(dir, "_internal", "prism.dll"), "prism.dll"} {
		if dll, err = syscall.LoadDLL(p); err == nil {
			break
		}
	}
	if err != nil {
		return err
	}
	initProc, err := dll.FindProc("prism_init")
	if err != nil {
		return err
	}
	for _, p := range []struct {
		name string
		dst  **syscall.Proc
	}{
		{"prism_registry_acquire_best", &acquireBest},
		{"prism_backend_initialize", &backendInit},
		{"prism_backend_output", &backendOutput},
		{"prism_backend_free", &backendFree},
	} {
		if *p.dst, err = dll.FindProc(p.name); err != nil {
			return err
		}
	}
	cfg := prismConfig{version: 4}
	r, _, _ := initProc.Call(uintptr(unsafe.Pointer(&cfg)))
	if r == 0 {
		return syscall.EINVAL
	}
	ctx = r
	return nil
}

// Speak says text through the active screen reader. Safe from any goroutine.
func Speak(text string, interrupt bool) {
	if text == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	history = append(history, text)
	if len(history) > 200 {
		history = history[len(history)-200:]
	}
	log.Printf("speak: %s", text)
	if ctx == 0 {
		return
	}
	b, _, _ := acquireBest.Call(ctx)
	if b == 0 {
		log.Printf("speech failed: no backend")
		return
	}
	defer backendFree.Call(b)
	if r, _, _ := backendInit.Call(b); r != prismOK && r != prismAlreadyInitialized {
		log.Printf("speech failed: initialize error %d", r)
		return
	}
	cs, err := syscall.BytePtrFromString(text)
	if err != nil {
		return
	}
	var flag uintptr
	if interrupt {
		flag = 1
	}
	if r, _, _ := backendOutput.Call(b, uintptr(unsafe.Pointer(cs)), flag); r != prismOK {
		log.Printf("speech failed: output error %d", r)
	}
}

func History() []string {
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), history...)
}
