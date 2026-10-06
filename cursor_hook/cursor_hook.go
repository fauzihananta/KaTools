// Package cursor_hook provides virtual cursor positioning via DLL injection into
// the target process. The injected DLL hooks GetCursorPos via IAT patching, so
// the game's cursor-polling code always sees our fake position without the
// physical cursor ever moving.
package cursor_hook

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32            = windows.NewLazyDLL("kernel32.dll")
	procLoadLibraryW    = kernel32.NewProc("LoadLibraryW")
	procVirtualAllocEx  = kernel32.NewProc("VirtualAllocEx")
	procVirtualFreeEx   = kernel32.NewProc("VirtualFreeEx")
	procWriteProcessMem = kernel32.NewProc("WriteProcessMemory")
	procCreateRemoteThr = kernel32.NewProc("CreateRemoteThread")

	procOpenFileMappingW = kernel32.NewProc("OpenFileMappingW")
	procMapViewOfFile    = kernel32.NewProc("MapViewOfFile")
	procUnmapViewOfFile  = kernel32.NewProc("UnmapViewOfFile")
	procOpenMutexW       = kernel32.NewProc("OpenMutexW")
	procWaitForSingleObj = kernel32.NewProc("WaitForSingleObject")
	procReleaseMutex     = kernel32.NewProc("ReleaseMutex")
)

const (
	FILE_MAP_ALL_ACCESS = 0xF001F
)

// Shared memory constants — must match dllmain.c
const (
	shmemName = "KaTools_CursorHook"
	mutexName = "KaTools_CursorHook_Mutex"
	shmemSize = 32 // sizeof(CURSOR_SHARED)
)

type cursorShared struct {
	Enabled int32
	X       int32
	Y       int32
	Pid     int32
}

// DLLPath returns the expected path of the hook DLL next to the executable.
func DLLPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "cursor_hook.dll")
}

// BuildDLL compiles the C source into cursor_hook.dll.
func BuildDLL() (string, error) {
	// Check if DLL already exists
	if _, err := os.Stat(DLLPath()); err == nil {
		return DLLPath(), nil
	}

	// Check if embedded DLL data exists (from gen_dll.go)
	if dllData := GetEmbeddedDLL(); len(dllData) > 0 {
		if err := os.WriteFile(DLLPath(), dllData, 0755); err != nil {
			return "", fmt.Errorf("failed to write embedded DLL: %w", err)
		}
		return DLLPath(), nil
	}

	// Try to build with gcc
	src := filepath.Join(sourceDir(), "dllmain.c")
	var cmd *exec.Cmd
	for _, gcc := range []string{"gcc", "x86_64-w64-mingw32-gcc"} {
		if path, err := exec.LookPath(gcc); err == nil {
			cmd = exec.Command(path, "-shared", "-O2", "-s",
				"-o", DLLPath(), src,
				"-luser32", "-lkernel32", "-ladvapi32")
			cmd.Dir = sourceDir()
			break
		}
	}

	if cmd == nil {
		return "", errors.New("no C compiler found (gcc or x86_64-w64-mingw32-gcc needed)")
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("DLL build failed: %v\n%s", err, out)
	}
	return DLLPath(), nil
}

func sourceDir() string {
	_, self, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(self), "..", "tools", "cursor_hook")
}

// GetEmbeddedDLL returns the embedded DLL bytes. If BuildDLL was run with
// go generate, the DLL is embedded as binary data.
func GetEmbeddedDLL() []byte {
	return embeddedDLL
}

//go:embed "cursor_hook.dll"
var embeddedDLL []byte

// Inject loads the hook DLL into the target process and returns a Controller.
func Inject(targetPID uint32) (*Controller, error) {
	// Verify DLL exists
	if _, err := os.Stat(DLLPath()); err != nil {
		return nil, fmt.Errorf("cursor_hook.dll not found at %s: %w", DLLPath(), err)
	}

	handle, err := windows.OpenProcess(
		windows.PROCESS_CREATE_THREAD|
			windows.PROCESS_VM_OPERATION|
			windows.PROCESS_VM_WRITE,
		false, targetPID)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess(%d): %w", targetPID, err)
	}

	pathBytes := []byte(DLLPath() + "\x00")
	remotePath, _, err := procVirtualAllocEx.Call(
		uintptr(handle), 0, uintptr(len(pathBytes)),
		windows.MEM_COMMIT|windows.MEM_RESERVE,
		windows.PAGE_READWRITE)
	if remotePath == 0 {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("VirtualAllocEx: %w", err)
	}

	if ok, _, err := procWriteProcessMem.Call(
		uintptr(handle), remotePath,
		uintptr(unsafe.Pointer(&pathBytes[0])),
		uintptr(len(pathBytes)), 0); ok == 0 {
		procVirtualFreeEx.Call(uintptr(handle), remotePath, 0, windows.MEM_RELEASE)
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("WriteProcessMemory: %w", err)
	}

	thread, _, err := procCreateRemoteThr.Call(
		uintptr(handle), 0, 0,
		uintptr(procLoadLibraryW.Addr()), remotePath, 0)
	if thread == 0 {
		procVirtualFreeEx.Call(uintptr(handle), remotePath, 0, windows.MEM_RELEASE)
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("CreateRemoteThread (LoadLibrary): %w", err)
	}

	windows.WaitForSingleObject(windows.Handle(thread), windows.INFINITE)
	windows.CloseHandle(windows.Handle(thread))
	procVirtualFreeEx.Call(uintptr(handle), remotePath, 0, windows.MEM_RELEASE)

	return &Controller{targetPID: targetPID, process: handle}, nil
}

// Controller manages the injected hook DLL via shared memory IPC.
type Controller struct {
	targetPID uint32
	process   windows.Handle
	mu        sync.Mutex
	enabled   bool
}

func openShmem() (windows.Handle, uintptr, error) {
	shmemNameW, _ := windows.UTF16PtrFromString(shmemName)
	shmem, _, err := procOpenFileMappingW.Call(
		uintptr(FILE_MAP_ALL_ACCESS), 0, uintptr(unsafe.Pointer(shmemNameW)))
	if shmem == 0 {
		return 0, 0, fmt.Errorf("OpenFileMappingW: %w", err)
	}

	data, _, err := procMapViewOfFile.Call(
		shmem, uintptr(FILE_MAP_ALL_ACCESS), 0, 0, shmemSize)
	if data == 0 {
		windows.CloseHandle(windows.Handle(shmem))
		return 0, 0, fmt.Errorf("MapViewOfFile: %w", err)
	}

	return windows.Handle(shmem), data, nil
}

func releaseShmem(shmem windows.Handle, data uintptr) {
	procUnmapViewOfFile.Call(data)
	windows.CloseHandle(shmem)
}

func acquireMutex() (windows.Handle, error) {
	mu, _, err := procOpenMutexW.Call(
		uintptr(windows.MUTEX_ALL_ACCESS), 0,
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(mutexName))))
	if mu == 0 {
		return 0, fmt.Errorf("OpenMutexW: %w", err)
	}
	procWaitForSingleObj.Call(mu, uintptr(^uint32(0))) // INFINITE
	return windows.Handle(mu), nil
}

func releaseMutexMu(mu windows.Handle) {
	procReleaseMutex.Call(uintptr(mu))
	windows.CloseHandle(mu)
}

// SetFakeCursor sets the fake cursor position. Pass screen coordinates.
func (c *Controller) SetFakeCursor(x, y int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	shmem, data, err := openShmem()
	if err != nil {
		return err
	}
	defer releaseShmem(shmem, data)

	mu, err := acquireMutex()
	if err != nil {
		return err
	}
	defer releaseMutexMu(mu)

	sh := (*cursorShared)(unsafe.Pointer(data))
	sh.X = int32(x)
	sh.Y = int32(y)
	sh.Pid = int32(os.Getpid())
	sh.Enabled = 1

	c.enabled = true
	return nil
}

// DisableFakeCursor stops faking the cursor position.
func (c *Controller) DisableFakeCursor() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disableUnsafe()
}

// Must be called with c.mu held.
func (c *Controller) disableUnsafe() error {
	shmem, data, err := openShmem()
	if err != nil {
		return err
	}
	defer releaseShmem(shmem, data)

	mu, err := acquireMutex()
	if err != nil {
		return err
	}
	defer releaseMutexMu(mu)

	sh := (*cursorShared)(unsafe.Pointer(data))
	sh.Enabled = 0
	c.enabled = false
	return nil
}

func (c *Controller) Enabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled
}

// Close releases resources.
func (c *Controller) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.enabled {
		_ = c.disableUnsafe()
	}
	windows.CloseHandle(c.process)
	return nil
}
