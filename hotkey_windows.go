//go:build windows

package main

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	hotkeyIDToggle = 1
	wmHotkey       = 0x0312
	wmSysKeyDown   = 0x0104
	whKeyboardLL   = 13
	vkF12          = 0x7B
	vkMenu         = 0x12 // VK_MENU (Alt key)
	llkhfAltDown   = 0x20

	modAlt      = 0x0001
	modNoRepeat = 0x4000
)

type hotkeyWndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  uintptr
	LpszClassName uintptr
	HIconSm       uintptr
}

type kbdllhookstruct struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

var (
	hotkeyUser32   = windows.NewLazySystemDLL("user32.dll")
	hotkeyKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	hotkeyProcRegisterHotKey      = hotkeyUser32.NewProc("RegisterHotKey")
	hotkeyProcUnregisterHotKey    = hotkeyUser32.NewProc("UnregisterHotKey")
	hotkeyProcRegisterClassExW    = hotkeyUser32.NewProc("RegisterClassExW")
	hotkeyProcCreateWindowExW     = hotkeyUser32.NewProc("CreateWindowExW")
	hotkeyProcDestroyWindow       = hotkeyUser32.NewProc("DestroyWindow")
	hotkeyProcGetMessageW         = hotkeyUser32.NewProc("GetMessageW")
	hotkeyProcTranslateMessage    = hotkeyUser32.NewProc("TranslateMessage")
	hotkeyProcDispatchMessageW    = hotkeyUser32.NewProc("DispatchMessageW")
	hotkeyProcDefWindowProcW      = hotkeyUser32.NewProc("DefWindowProcW")
	hotkeyProcGetModuleHandleW    = hotkeyKernel32.NewProc("GetModuleHandleW")
	hotkeyProcSetWindowsHookExW   = hotkeyUser32.NewProc("SetWindowsHookExW")
	hotkeyProcUnhookWindowsHookEx = hotkeyUser32.NewProc("UnhookWindowsHookEx")
	hotkeyProcCallNextHookEx      = hotkeyUser32.NewProc("CallNextHookEx")
	hotkeyProcPostMessageW        = hotkeyUser32.NewProc("PostMessageW")
	hotkeyProcGetAsyncKeyState    = hotkeyUser32.NewProc("GetAsyncKeyState")

	hotkeyRM      *RuntimeManager
	hotkeyRMmu    sync.RWMutex
	hotkeyHwnd    uintptr
	hotkeyHook    uintptr
	hotkeyHookMu  sync.Mutex
	hookInstalled bool // true if hook is currently active

	// Debounce and concurrency guards to prevent rapid key-repeat flapping
	hookLastToggle time.Time
	hookToggleMu   sync.Mutex
	hotkeyToggling int32 // atomic: 1 while a Start/Stop operation is in flight
	hotkeyLastRun  time.Time
	hotkeyRunMu    sync.Mutex
)

func hotkeyUtf16Ptr(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

func hotkeyLog(format string, args ...interface{}) {
	msg := fmt.Sprintf("[Hotkey] "+format, args...)
	if f, err := os.OpenFile("katools_hotkey.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		fmt.Fprintln(f, msg)
		f.Close()
	}
}

// SetHotkeyRuntimeManager sets the RuntimeManager used by the hotkey handler.
func SetHotkeyRuntimeManager(rm *RuntimeManager) {
	hotkeyRMmu.Lock()
	hotkeyRM = rm
	hotkeyRMmu.Unlock()
}

// RunHotkeyServer starts the global hotkey server in a background goroutine.
func RunHotkeyServer() {
	go hotkeyWindowLoop()
}

var hotkeyWndProcVar = syscall.NewCallback(func(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	if msg == wmHotkey {
		hotkeyLog("Hotkey triggered: wParam=%d", wParam)
		// RegisterHotKey and a fallback hook must never turn one physical key
		// press into Stop followed immediately by Start. Keep a final debounce
		// at the single dispatch point as a defence against duplicate messages.
		hotkeyRunMu.Lock()
		now := time.Now()
		duplicate := now.Sub(hotkeyLastRun) < 750*time.Millisecond
		if !duplicate {
			hotkeyLastRun = now
		}
		hotkeyRunMu.Unlock()
		if duplicate {
			hotkeyLog("Toggle ignored: duplicate WM_HOTKEY")
			return 0
		}
		// Prevent concurrent Start/Stop tasks if previous is still working
		if !atomic.CompareAndSwapInt32(&hotkeyToggling, 0, 1) {
			hotkeyLog("Toggle ignored: previous start/stop action still in progress")
			return 0
		}

		// Run in a separate goroutine so hotkeyWindowLoop is never blocked.
		// If the message loop blocks, Windows will timeout and silently unhook WH_KEYBOARD_LL.
		go func() {
			hotkeyRMmu.RLock()
			rm := hotkeyRM
			hotkeyRMmu.RUnlock()

			if rm == nil {
				atomic.StoreInt32(&hotkeyToggling, 0)
				return
			}

			action := rm.ToggleFromHotkey()
			hotkeyLog("Toggle via Alt+F12: %s", action)
			atomic.StoreInt32(&hotkeyToggling, 0)
			return

			/* Legacy runtime-wide toggle path, retained temporarily only as
			 * reference for the migration to ToggleFromHotkey.
			if rm.IsRunning() {
				hotkeyLog("Toggle: stopping bot via Alt+F12")
				// Stop stays asynchronous so the Windows message loop remains
				// responsive. The toggle guard is released only by the goroutine
				// that actually finishes teardown; this prevents overlapping
				// runtimes if cleanup is slower than the diagnostic timeout.
				go func() {
					stopped := make(chan struct{})
					go func() {
						rm.Stop()
						close(stopped)
						atomic.StoreInt32(&hotkeyToggling, 0)
					}()
					select {
					case <-stopped:
						hotkeyLog("Toggle: bot stopped cleanly")
					case <-time.After(3 * time.Second):
						hotkeyLog("Toggle: STOP TIMED OUT after 3s — forcing hotkey reset")
						appendOCRLog("HOTKEY STOP TIMED OUT | rm.Stop() did not return within 3s; forcing hotkeyToggling=0")
					}
				}()
			} else {
				hotkeyLog("Toggle: starting bot via Alt+F12")
				rm.HotkeyStart()
				atomic.StoreInt32(&hotkeyToggling, 0)
			}
			*/
		}()
		return 0
	}

	ret, _, _ := hotkeyProcDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
})

// lowLevelKeyboardProc intercepts Alt+F12 before other apps.
// Debounces events within 500ms to eliminate typematic auto-repeat and accidental double-clicks.
var lowLevelKeyboardProc = syscall.NewCallback(func(code int32, wParam uintptr, lParam uintptr) uintptr {
	if code >= 0 && (wParam == wmKeyDown || wParam == wmSysKeyDown) {
		lps := (*kbdllhookstruct)(unsafe.Pointer(lParam))
		if lps.VkCode == vkF12 {
			altDown := (lps.Flags & llkhfAltDown) != 0
			if !altDown {
				ret, _, _ := hotkeyProcGetAsyncKeyState.Call(vkMenu)
				altDown = (ret & 0x8000) != 0
			}
			if altDown {
				hookToggleMu.Lock()
				now := time.Now()
				tooSoon := now.Sub(hookLastToggle) < 500*time.Millisecond
				if !tooSoon {
					hookLastToggle = now
				}
				hookToggleMu.Unlock()

				if !tooSoon {
					hotkeyHookMu.Lock()
					hwnd := hotkeyHwnd
					hotkeyHookMu.Unlock()
					if hwnd != 0 {
						hotkeyProcPostMessageW.Call(hwnd, wmHotkey, hotkeyIDToggle, 0)
					}
				}
				// Consume Alt+F12 so other apps don't see it
				return 1
			}
		}
	}
	// Pass every unhandled key through the system hook chain.
	ret, _, _ := hotkeyProcCallNextHookEx.Call(0, uintptr(code), wParam, lParam)
	return ret
})

func hotkeyWindowLoop() {
	instance, _, _ := hotkeyProcGetModuleHandleW.Call(0)

	className := hotkeyUtf16Ptr("KaToolsHotkeyClass")
	wc := hotkeyWndClassEx{
		CbSize:        uint32(unsafe.Sizeof(hotkeyWndClassEx{})),
		LpfnWndProc:   hotkeyWndProcVar,
		HInstance:     instance,
		HbrBackground: 6,
		LpszClassName: uintptr(unsafe.Pointer(className)),
	}
	if ret, _, err := hotkeyProcRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		hotkeyLog("RegisterClassEx failed: %v", err)
		return
	}

	hwnd, _, _ := hotkeyProcCreateWindowExW.Call(
		0, // dwExStyle
		uintptr(unsafe.Pointer(className)),
		0,          // lpWindowName
		0,          // dwStyle
		0, 0, 0, 0, // x,y,w,h
		0,        // hWndParent
		0,        // hMenu
		instance, // hInstance
		0,        // lpParam
	)
	if hwnd == 0 {
		hotkeyLog("CreateWindowEx failed")
		return
	}

	// Store hwnd for the hook to use
	hotkeyHookMu.Lock()
	hotkeyHwnd = hwnd
	hotkeyHookMu.Unlock()

	// RegisterHotKey is the primary path. Installing a low-level hook at the
	// same time produced two WM_HOTKEY messages for one key press and could Stop
	// then immediately Start the runtime again. The hook is now fallback-only.
	registered := false
	if ok, _, _ := hotkeyProcRegisterHotKey.Call(hwnd, uintptr(hotkeyIDToggle), modAlt|modNoRepeat, vkF12); ok != 0 {
		registered = true
		hotkeyLog("RegisterHotKey Alt+F12 = Start/Stop")
	} else if ok, _, _ := hotkeyProcRegisterHotKey.Call(hwnd, uintptr(hotkeyIDToggle), modAlt, vkF12); ok != 0 {
		registered = true
		hotkeyLog("RegisterHotKey Alt+F12 = Start/Stop")
	} else {
		hotkeyLog("RegisterHotKey Alt+F12 taken; installing fallback hook")
	}

	if !registered {
		hook, _, _ := hotkeyProcSetWindowsHookExW.Call(
			whKeyboardLL,
			lowLevelKeyboardProc,
			instance,
			0,
		)
		if hook != 0 {
			hotkeyHookMu.Lock()
			hotkeyHook = hook
			hookInstalled = true
			hotkeyHookMu.Unlock()
			hotkeyLog("Fallback keyboard hook installed (global)")
		} else {
			hotkeyLog("Fallback keyboard hook install failed")
		}
	}

	var msg struct{ Hwnd, Message, WParam, LParam, Time, Pt int64 }

	for {
		ret, _, _ := hotkeyProcGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret <= 0 {
			break
		}
		hotkeyProcTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		hotkeyProcDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	// Cleanup
	hotkeyHookMu.Lock()
	if hotkeyHook != 0 {
		hotkeyProcUnhookWindowsHookEx.Call(hotkeyHook)
		hotkeyHook = 0
		hookInstalled = false
	}
	hotkeyHookMu.Unlock()

	if registered {
		hotkeyProcUnregisterHotKey.Call(hwnd, hotkeyIDToggle)
	}
	hotkeyProcDestroyWindow.Call(hwnd)
}
