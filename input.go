package main

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var errCursorAPI = errors.New("cursor API failure")

const (
	wmKeyDown                 = 0x0100
	wmKeyUp                   = 0x0101
	wmMouseMove               = 0x0200
	wmLButtonDown             = 0x0201
	wmLButtonUp               = 0x0202
	mkLButton                 = 0x0001
	smtoAbortIfHung           = 0x0002
	cwpSkipInvisible          = 0x0001
	cwpSkipDisabled           = 0x0002
	cwpSkipTransparent        = 0x0004
	virtualClickCursorTimeout = 250 * time.Millisecond
	virtualClickPollInterval  = 2 * time.Millisecond
	virtualClickMoveSettle    = 40 * time.Millisecond
	virtualClickButtonHold    = 50 * time.Millisecond
	virtualClickReleaseSettle = 40 * time.Millisecond
)

var (
	user32DLL                  = syscall.NewLazyDLL("user32.dll")
	procSendMessageTimeoutW    = user32DLL.NewProc("SendMessageTimeoutW")
	procChildWindowFromPointEx = user32DLL.NewProc("ChildWindowFromPointEx")
	procMapWindowPoints        = user32DLL.NewProc("MapWindowPoints")
	procGetClientRect          = user32DLL.NewProc("GetClientRect")
	procClientToScreen         = user32DLL.NewProc("ClientToScreen")
	procSetCursorPos           = user32DLL.NewProc("SetCursorPos")
	procGetCursorPos           = user32DLL.NewProc("GetCursorPos")
	virtualClickMu             sync.Mutex
	clickDiagnosticMu          sync.Mutex
	clickDiagnosticsLogged     = make(map[string]struct{})
)

type mousePoint struct {
	X int32
	Y int32
}

func pressKeyToWindow(hwnd uintptr, vk uintptr) bool {
	if hwnd == 0 || vk == 0 {
		return false
	}

	// Kathana consumes a posted down/up pair as normal text while its chat
	// editor is active. A synchronous WM_KEYDOWN is handled by the game's
	// command path instead: it still triggers skills, but does not inject the
	// key into the chat editor. Keep this as one message -- a synthetic key-up
	// is neither needed nor wanted for these one-shot actions.
	var result uintptr
	ok, _, callErr := procSendMessageTimeoutW.Call(
		hwnd,
		uintptr(wmKeyDown),
		vk,
		0,
		smtoAbortIfHung,
		1000,
		uintptr(unsafe.Pointer(&result)),
	)
	if ok == 0 {
		if callErr != syscall.Errno(0) {
			fmt.Printf("[Input] WM_KEYDOWN to window 0x%X failed: %v\n", hwnd, callErr)
		} else {
			fmt.Printf("[Input] WM_KEYDOWN to window 0x%X timed out\n", hwnd)
		}
		return false
	}
	return true
}

func resolveMouseMessageTarget(hwnd uintptr, x, y int) (uintptr, int, int) {
	target := hwnd
	point := mousePoint{X: int32(x), Y: int32(y)}
	flags := uintptr(cwpSkipInvisible | cwpSkipDisabled | cwpSkipTransparent)

	for depth := 0; depth < 16; depth++ {
		child, _, _ := procChildWindowFromPointEx.Call(target, uintptr(unsafe.Pointer(&point)), flags)
		if child == 0 || child == target {
			break
		}
		procMapWindowPoints.Call(target, child, uintptr(unsafe.Pointer(&point)), 1)
		target = child
	}
	return target, int(point.X), int(point.Y)
}

// clickWindowClientPoint sends a click to the selected window and restores the
// user's cursor after the target has had time to process the click. Some games
// require the real cursor to remain over the target while handling messages.
func clickWindowClientPoint(hwnd uintptr, x, y int) error {
	return clickWindowClientPointReference(hwnd, x, y, 0, 0)
}

func clickWindowClientPointReference(hwnd uintptr, x, y, referenceWidth, referenceHeight int) error {
	virtualClickMu.Lock()
	defer virtualClickMu.Unlock()
	savedX, savedY := x, y
	if hwnd == 0 || x < 0 || y < 0 || x > 0x7fff || y > 0x7fff {
		return fmt.Errorf("invalid click target or client point %d,%d", x, y)
	}
	var clientRect struct{ Left, Top, Right, Bottom int32 }
	if ret, _, err := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&clientRect))); ret == 0 {
		if err != syscall.Errno(0) {
			return fmt.Errorf("could not read target client area: %w", err)
		}
		return fmt.Errorf("could not read target client area")
	}
	clientWidth := int(clientRect.Right - clientRect.Left)
	clientHeight := int(clientRect.Bottom - clientRect.Top)
	if clientWidth <= 0 || clientHeight <= 0 {
		return fmt.Errorf("selected target has invalid client area %dx%d", clientWidth, clientHeight)
	}
	if referenceWidth > 0 && referenceHeight > 0 {
		x = int(int64(x) * int64(clientWidth) / int64(referenceWidth))
		y = int(int64(y) * int64(clientHeight) / int64(referenceHeight))
	}
	if x < 0 || y < 0 || x >= clientWidth || y >= clientHeight {
		return fmt.Errorf("saved click point %d,%d maps outside target client area %dx%d", x, y, clientWidth, clientHeight)
	}

	messageHWND, messageX, messageY := resolveMouseMessageTarget(hwnd, x, y)
	if messageHWND == 0 || messageX < 0 || messageY < 0 || messageX > 0x7fff || messageY > 0x7fff {
		return fmt.Errorf("click point could not be mapped to the selected window")
	}

	var originalCursor mousePoint
	if ok, _, callErr := procGetCursorPos.Call(uintptr(unsafe.Pointer(&originalCursor))); ok == 0 {
		if callErr != syscall.Errno(0) {
			return fmt.Errorf("%w: could not read current cursor position: %v", errCursorAPI, callErr)
		}
		return fmt.Errorf("%w: could not read current cursor position", errCursorAPI)
	}
	screenPoint := mousePoint{X: int32(x), Y: int32(y)}
	if ok, _, callErr := procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&screenPoint))); ok == 0 {
		if callErr != syscall.Errno(0) {
			return fmt.Errorf("could not map click point to screen coordinates: %w", callErr)
		}
		return fmt.Errorf("could not map click point to screen coordinates")
	}
	if ok, _, callErr := procSetCursorPos.Call(uintptr(screenPoint.X), uintptr(screenPoint.Y)); ok == 0 {
		if callErr != syscall.Errno(0) {
			return fmt.Errorf("%w: could not move cursor to click point: %v", errCursorAPI, callErr)
		}
		return fmt.Errorf("%w: could not move cursor to click point", errCursorAPI)
	}
	defer procSetCursorPos.Call(uintptr(originalCursor.X), uintptr(originalCursor.Y))
	if err := waitForCursorPosition(screenPoint); err != nil {
		return fmt.Errorf("cursor did not reach click point %d,%d: %w", screenPoint.X, screenPoint.Y, err)
	}
	logVirtualClickPointOnce(hwnd, savedX, savedY, x, y, referenceWidth, referenceHeight, clientWidth, clientHeight, messageHWND, messageX, messageY, int(screenPoint.X), int(screenPoint.Y))

	lParam := uintptr(uint16(messageX)) | uintptr(uint16(messageY))<<16
	send := func(message uint32, keyState uintptr) error {
		var result uintptr
		ok, _, callErr := procSendMessageTimeoutW.Call(
			messageHWND, uintptr(message), keyState, lParam,
			smtoAbortIfHung, 1000, uintptr(unsafe.Pointer(&result)),
		)
		if ok == 0 {
			if callErr != syscall.Errno(0) {
				return fmt.Errorf("mouse message 0x%X failed: %w", message, callErr)
			}
			return fmt.Errorf("mouse message 0x%X timed out", message)
		}
		return nil
	}
	if err := send(wmMouseMove, 0); err != nil {
		return err
	}
	// Give games that poll the real cursor (instead of trusting only lParam)
	// time to observe the move before the button-down message arrives.
	time.Sleep(virtualClickMoveSettle)
	if !cursorAt(screenPoint) {
		if ok, _, callErr := procSetCursorPos.Call(uintptr(screenPoint.X), uintptr(screenPoint.Y)); ok == 0 {
			if callErr != syscall.Errno(0) {
				return fmt.Errorf("%w: could not restore cursor to click point: %v", errCursorAPI, callErr)
			}
			return fmt.Errorf("%w: could not restore cursor to click point", errCursorAPI)
		}
		if err := waitForCursorPosition(screenPoint); err != nil {
			return fmt.Errorf("cursor left click point before button-down: %w", err)
		}
		if err := send(wmMouseMove, 0); err != nil {
			return err
		}
		time.Sleep(virtualClickMoveSettle)
		if !cursorAt(screenPoint) {
			return fmt.Errorf("cursor moved away from click point before button-down")
		}
	}
	if err := send(wmLButtonDown, mkLButton); err != nil {
		_ = send(wmLButtonUp, 0)
		return err
	}
	time.Sleep(virtualClickButtonHold)
	if err := send(wmLButtonUp, 0); err != nil {
		// Retry release once so a transient timeout cannot leave the game's
		// button state stuck down. Cursor restoration remains deferred below.
		time.Sleep(virtualClickReleaseSettle)
		if retryErr := send(wmLButtonUp, 0); retryErr != nil {
			return fmt.Errorf("%v; release retry failed: %w", err, retryErr)
		}
		return err
	}
	time.Sleep(virtualClickReleaseSettle)
	return nil
}

func cursorAt(expected mousePoint) bool {
	var current mousePoint
	if ok, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&current))); ok == 0 {
		return false
	}
	return current == expected
}

func waitForCursorPosition(expected mousePoint) error {
	deadline := time.Now().Add(virtualClickCursorTimeout)
	for time.Now().Before(deadline) {
		if cursorAt(expected) {
			return nil
		}
		time.Sleep(virtualClickPollInterval)
	}
	return fmt.Errorf("position verification timed out after %s", virtualClickCursorTimeout)
}

func logVirtualClickPointOnce(hwnd uintptr, savedX, savedY, x, y, referenceWidth, referenceHeight, clientWidth, clientHeight int, messageHWND uintptr, messageX, messageY, screenX, screenY int) {
	key := fmt.Sprintf("%X:%d:%d:%d:%d:%d:%d", hwnd, savedX, savedY, referenceWidth, referenceHeight, clientWidth, clientHeight)
	clickDiagnosticMu.Lock()
	if _, exists := clickDiagnosticsLogged[key]; exists {
		clickDiagnosticMu.Unlock()
		return
	}
	clickDiagnosticsLogged[key] = struct{}{}
	clickDiagnosticMu.Unlock()
	appendOCRLog("VIRTUAL CLICK POINT | HWND=%X | Saved=%d,%d in %dx%d | MappedClient=%d,%d in %dx%d | DeliveryHWND=%X Client=%d,%d | Screen=%d,%d", hwnd, savedX, savedY, referenceWidth, referenceHeight, x, y, clientWidth, clientHeight, messageHWND, messageX, messageY, screenX, screenY)
}
