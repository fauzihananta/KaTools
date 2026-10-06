//go:build windows

package main

import (
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	hotkeyIDToggle = 1

	wmHotkey     = 0x0312
	whKeyboardLL = 13
	vkF12        = 0x7B
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

	hotkeyProcRegisterHotKey    = hotkeyUser32.NewProc("RegisterHotKey")
	hotkeyProcUnregisterHotKey  = hotkeyUser32.NewProc("UnregisterHotKey")
	hotkeyProcRegisterClassExW  = hotkeyUser32.NewProc("RegisterClassExW")
	hotkeyProcCreateWindowExW   = hotkeyUser32.NewProc("CreateWindowExW")
	hotkeyProcDestroyWindow     = hotkeyUser32.NewProc("DestroyWindow")
	hotkeyProcGetMessageW       = hotkeyUser32.NewProc("GetMessageW")
	hotkeyProcTranslateMessage  = hotkeyUser32.NewProc("TranslateMessage")
	hotkeyProcDispatchMessageW  = hotkeyUser32.NewProc("DispatchMessageW")
	hotkeyProcDefWindowProcW    = hotkeyUser32.NewProc("DefWindowProcW")
	hotkeyProcGetModuleHandleW  = hotkeyKernel32.NewProc("GetModuleHandleW")
	hotkeyProcSetWindowsHookExW = hotkeyUser32.NewProc("SetWindowsHookExW")
	hotkeyProcUnhookWindowsHookEx = hotkeyUser32.NewProc("UnhookWindowsHookEx")
	hotkeyProcPostMessageW     = hotkeyUser32.NewProc("PostMessageW")
	hotkeyProcGetAsyncKeyState = hotkeyUser32.NewProc("GetAsyncKeyState")

	hotkeyRM     *RuntimeManager
	hotkeyRMmu   sync.RWMutex
	hotkeyHwnd   uintptr
	hotkeyHook   uintptr
	hotkeyHookMu sync.Mutex
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
		hotkeyRMmu.RLock()
		rm := hotkeyRM
		hotkeyRMmu.RUnlock()

		if rm != nil {
			if rm.IsRunning() {
				rm.Stop()
			} else {
				rm.HotkeyStart()
			}
		}
		return 0
	}
	ret, _, _ := hotkeyProcDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
})

// lowLevelKeyboardProc runs in the same thread as the hook — it's safe to call
// into the app from here without deadlock.
var lowLevelKeyboardProc = syscall.NewCallback(func(code int32, wParam uintptr, lParam uintptr) uintptr {
	if code >= 0 && wParam == wmKeyDown {
		lps := (*kbdllhookstruct)(unsafe.Pointer(lParam))
		if lps.VkCode == vkF12 {
			hotkeyHookMu.Lock()
			hwnd := hotkeyHwnd
			hotkeyHookMu.Unlock()
			if hwnd != 0 {
				hotkeyProcPostMessageW.Call(hwnd, wmHotkey, hotkeyIDToggle, 0)
			}
			// Consume F12 so other apps don't see it
			return 1
		}
	}
	// Pass to next hook
	return 0
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
		0,                          // lpWindowName
		0,                          // dwStyle
		0, 0, 0, 0, // x,y,w,h
		0, // hWndParent
		0, // hMenu
		instance,
		0, // lpParam
	)
	if hwnd == 0 {
		hotkeyLog("CreateWindowEx failed")
		return
	}

	// Store hwnd for the hook to use
	hotkeyHookMu.Lock()
	hotkeyHwnd = hwnd
	hotkeyHookMu.Unlock()

	// Install low-level keyboard hook FIRST to intercept F12 before other apps.
	// This runs in our own thread so it's safe from the start.
	hook, _, _ := hotkeyProcSetWindowsHookExW.Call(
		whKeyboardLL,
		lowLevelKeyboardProc,
		instance,
		0, // threadID=0 → global hook
	)
	if hook != 0 {
		hotkeyHook = hook
		hotkeyLog("Keyboard hook installed (global) - F12 intercepted")
	} else {
		hotkeyLog("Keyboard hook install failed - F12 interception unavailable")
	}

	// Try normal RegisterHotKey as well (may succeed on fresh boot)
	if ok, _, _ := hotkeyProcRegisterHotKey.Call(hwnd, uintptr(hotkeyIDToggle), 0, vkF12); ok != 0 {
		hotkeyLog("RegisterHotKey F12 = Start/Stop")
	} else {
		hotkeyLog("RegisterHotKey F12 taken (hook will handle it)")
	}

	var msg struct{ Hwnd, Message, WParam, LParam, Time, Pt int64 }
	for {
		ret, _, _ := hotkeyProcGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 {
			break
		}
		hotkeyProcTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		hotkeyProcDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	// Cleanup
	if hotkeyHook != 0 {
		hotkeyProcUnhookWindowsHookEx.Call(hotkeyHook)
	}
	hotkeyProcUnregisterHotKey.Call(hwnd, hotkeyIDToggle)
	hotkeyProcDestroyWindow.Call(hwnd)
}
