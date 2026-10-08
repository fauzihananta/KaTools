package main

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"KaTools/cursor_hook"
)

var (
	errCursorAPI      = errors.New("cursor API failure")
	errClickCanceled  = errors.New("virtual click canceled")
	errClickPreempted = errors.New("virtual click preempted by higher-priority input")
)

const (
	wmKeyDown                  = 0x0100
	wmKeyUp                    = 0x0101
	wmMouseMove                = 0x0200
	wmLButtonDown              = 0x0201
	wmLButtonUp                = 0x0202
	mkLButton                  = 0x0001
	smtoAbortIfHung            = 0x0002
	cwpSkipInvisible           = 0x0001
	cwpSkipDisabled            = 0x0002
	cwpSkipTransparent         = 0x0004
	virtualClickCursorTimeout  = 250 * time.Millisecond
	virtualClickMessageTimeout = 250 * time.Millisecond
	virtualClickPollInterval   = 2 * time.Millisecond
	virtualClickMoveSettle     = 40 * time.Millisecond
	virtualClickButtonHold     = 50 * time.Millisecond
	virtualClickReleaseSettle  = 40 * time.Millisecond
	clipCursorLockSize         = 10 // pixel radius around target to lock cursor
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
	procClipCursor             = user32DLL.NewProc("ClipCursor")
	procGetClipCursor          = user32DLL.NewProc("GetClipCursor")
	virtualClickMu             sync.Mutex
	clickDiagnosticMu          sync.Mutex
	clickDiagnosticsLogged     = make(map[string]struct{})

	// Global cursor hook controller for virtual cursor positioning.
	// When set, every virtual click will fake GetCursorPos inside the target
	// process so the game sees the cursor at the target without the physical
	// cursor ever moving.
	globalCursorHook   *cursor_hook.Controller
	globalCursorHookMu sync.RWMutex
)

type mousePoint struct {
	X int32
	Y int32
}

type rect struct {
	Left, Top, Right, Bottom int32
}

// SetGlobalCursorHook stores the cursor hook controller used for virtual cursor positioning.
func SetGlobalCursorHook(ctrl *cursor_hook.Controller) {
	globalCursorHookMu.Lock()
	defer globalCursorHookMu.Unlock()
	globalCursorHook = ctrl
}

// GetGlobalCursorHook returns the current cursor hook controller.
func GetGlobalCursorHook() *cursor_hook.Controller {
	globalCursorHookMu.RLock()
	defer globalCursorHookMu.RUnlock()
	return globalCursorHook
}

// trySetFakeCursor attempts to set the fake cursor position. Errors are non-fatal.
func trySetFakeCursor(x, y int) {
	ctrl := GetGlobalCursorHook()
	if ctrl == nil {
		return
	}
	_ = ctrl.SetFakeCursor(x, y)
}

// tryClearFakeCursor attempts to disable the fake cursor. Errors are non-fatal.
func tryClearFakeCursor() {
	ctrl := GetGlobalCursorHook()
	if ctrl == nil {
		return
	}
	_ = ctrl.DisableFakeCursor()
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
	return clickWindowClientPointReferenceLocked(hwnd, x, y, referenceWidth, referenceHeight)
}

// clickWindowClientPointReferenceCancelable prevents stopped skill workers
// from draining through virtualClickMu and clicking after the user requested
// Stop. The second cancellation check closes the race between TryLock and the
// first Windows cursor operation.
func clickWindowClientPointReferenceCancelable(stop <-chan struct{}, hwnd uintptr, x, y, referenceWidth, referenceHeight int) error {
	return clickWindowClientPointReferencePriority(stop, nil, hwnd, x, y, referenceWidth, referenceHeight)
}

// clickWindowClientPointReferencePriority is the non-queued priority gate for
// Click Method. Regular attack timers still race independently, but a waiting
// attack abandons its cast as soon as Emergency/AutoPot/Buff becomes pending.
// The second check after TryLock prevents an attack from stealing the physical
// cursor lane in the small window between the first check and lock acquisition.
func clickWindowClientPointReferencePriority(stop <-chan struct{}, preempted func() bool, hwnd uintptr, x, y, referenceWidth, referenceHeight int) error {
	for {
		select {
		case <-stop:
			return errClickCanceled
		default:
		}
		if preempted != nil && preempted() {
			return errClickPreempted
		}
		if virtualClickMu.TryLock() {
			break
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-stop:
			if !timer.Stop() {
				<-timer.C
			}
			return errClickCanceled
		case <-timer.C:
		}
	}
	defer virtualClickMu.Unlock()
	select {
	case <-stop:
		return errClickCanceled
	default:
	}
	if preempted != nil && preempted() {
		return errClickPreempted
	}
	return clickWindowClientPointReferenceLocked(hwnd, x, y, referenceWidth, referenceHeight)
}

func clickWindowClientPointReferenceLocked(hwnd uintptr, x, y, referenceWidth, referenceHeight int) error {
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

	screenPoint := mousePoint{X: int32(x), Y: int32(y)}
	if ok, _, callErr := procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&screenPoint))); ok == 0 {
		if callErr != syscall.Errno(0) {
			return fmt.Errorf("could not map click point to screen coordinates: %w", callErr)
		}
		return fmt.Errorf("could not map click point to screen coordinates")
	}
	logVirtualClickPointOnce(hwnd, savedX, savedY, x, y, referenceWidth, referenceHeight, clientWidth, clientHeight, messageHWND, messageX, messageY, int(screenPoint.X), int(screenPoint.Y))

	var originalCursor mousePoint
	if ok, _, callErr := procGetCursorPos.Call(uintptr(unsafe.Pointer(&originalCursor))); ok == 0 {
		if callErr != syscall.Errno(0) {
			return fmt.Errorf("%w: could not read current cursor position: %v", errCursorAPI, callErr)
		}
		return fmt.Errorf("%w: could not read current cursor position", errCursorAPI)
	}
	// Lock cursor to a small rect around the target so player mouse movements
	// cannot drag the cursor away during the click sequence.
	var origClip rect
	if ret, _, err := procGetClipCursor.Call(uintptr(unsafe.Pointer(&origClip))); ret == 0 {
		if err != syscall.Errno(0) {
			fmt.Printf("[Input] GetClipCursor failed: %v\n", err)
		}
	}
	clipRect := rect{
		Left:   screenPoint.X - clipCursorLockSize,
		Top:    screenPoint.Y - clipCursorLockSize,
		Right:  screenPoint.X + clipCursorLockSize,
		Bottom: screenPoint.Y + clipCursorLockSize,
	}
	if ret, _, err := procClipCursor.Call(uintptr(unsafe.Pointer(&clipRect))); ret == 0 {
		if err != syscall.Errno(0) {
			fmt.Printf("[Input] ClipCursor failed: %v\n", err)
		}
	}
	if ok, _, callErr := procSetCursorPos.Call(uintptr(screenPoint.X), uintptr(screenPoint.Y)); ok == 0 {
		procClipCursor.Call(uintptr(unsafe.Pointer(&origClip)))
		if callErr != syscall.Errno(0) {
			return fmt.Errorf("%w: could not move cursor to click point: %v", errCursorAPI, callErr)
		}
		return fmt.Errorf("%w: could not move cursor to click point", errCursorAPI)
	}
	defer procSetCursorPos.Call(uintptr(originalCursor.X), uintptr(originalCursor.Y))
	if err := waitForCursorPosition(screenPoint); err != nil {
		procClipCursor.Call(uintptr(unsafe.Pointer(&origClip)))
		return fmt.Errorf("cursor did not reach click point %d,%d: %w", screenPoint.X, screenPoint.Y, err)
	}

	lParam := uintptr(uint16(messageX)) | uintptr(uint16(messageY))<<16
	send := func(message uint32, keyState uintptr) error {
		var result uintptr
		ok, _, callErr := procSendMessageTimeoutW.Call(
			messageHWND, uintptr(message), keyState, lParam,
			smtoAbortIfHung, uintptr(virtualClickMessageTimeout.Milliseconds()), uintptr(unsafe.Pointer(&result)),
		)
		if ok == 0 {
			if callErr != syscall.Errno(0) {
				return fmt.Errorf("mouse message 0x%X failed: %w", message, callErr)
			}
			return fmt.Errorf("mouse message 0x%X timed out", message)
		}
		return nil
	}

	var clickErr error
	if err := send(wmMouseMove, 0); err != nil {
		clickErr = err
	} else if err := send(wmLButtonDown, mkLButton); err != nil {
		_ = send(wmLButtonUp, 0)
		clickErr = err
	} else {
		time.Sleep(virtualClickButtonHold)
		if err := send(wmLButtonUp, 0); err != nil {
			time.Sleep(virtualClickReleaseSettle)
			if retryErr := send(wmLButtonUp, 0); retryErr != nil {
				clickErr = fmt.Errorf("%v; release retry failed: %w", err, retryErr)
			} else {
				clickErr = err
			}
		}
		time.Sleep(virtualClickReleaseSettle)
	}

	tryClearFakeCursor()
	// Restore original clip rect so the player's cursor is free again.
	procClipCursor.Call(uintptr(unsafe.Pointer(&origClip)))
	return clickErr
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

// clickWindowClipped is like clickWindowClientPoint but uses ClipCursor to
// lock the physical cursor to a small area around the target during the click.
// This lets games that poll GetCursorPos see a valid position at the target
// while preventing the cursor from drifting far away. The user can still move
// the mouse within the clipped area or yank it out to interrupt.
func clickWindowClipped(hwnd uintptr, x, y int) error {
	return clickWindowClippedReference(hwnd, x, y, 0, 0)
}

func clickWindowClippedReference(hwnd uintptr, x, y, referenceWidth, referenceHeight int) error {
	virtualClickMu.Lock()
	defer virtualClickMu.Unlock()

	if hwnd == 0 || x < 0 || y < 0 || x > 0x7fff || y > 0x7fff {
		return fmt.Errorf("invalid click target or client point %d,%d", x, y)
	}

	// Save original clip rect so we can restore it later
	var origClip rect
	if ret, _, err := procGetClipCursor.Call(uintptr(unsafe.Pointer(&origClip))); ret == 0 {
		if err != syscall.Errno(0) {
			fmt.Printf("[Input] GetClipCursor failed: %v\n", err)
		}
		// Continue anyway; we'll restore by passing nil later
	}

	// Convert client point to screen coordinates
	screenPoint := mousePoint{X: int32(x), Y: int32(y)}
	if ok, _, callErr := procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&screenPoint))); ok == 0 {
		if callErr != syscall.Errno(0) {
			return fmt.Errorf("could not map click point to screen: %v", callErr)
		}
		return fmt.Errorf("could not map click point to screen")
	}

	// Clip cursor to a tiny rect centered on target — cursor stays near click point
	// but user can still yank it to interrupt, and it won't drift across screen
	clipRect := rect{
		Left:   screenPoint.X - clipCursorLockSize,
		Top:    screenPoint.Y - clipCursorLockSize,
		Right:  screenPoint.X + clipCursorLockSize,
		Bottom: screenPoint.Y + clipCursorLockSize,
	}
	if ret, _, err := procClipCursor.Call(uintptr(unsafe.Pointer(&clipRect))); ret == 0 {
		if err != syscall.Errno(0) {
			return fmt.Errorf("ClipCursor failed: %v", err)
		}
		return fmt.Errorf("ClipCursor failed")
	}

	// Move cursor to exact target position (still locked within clip rect)
	if ok, _, callErr := procSetCursorPos.Call(uintptr(screenPoint.X), uintptr(screenPoint.Y)); ok == 0 {
		procClipCursor.Call(0) // release clip before returning
		if callErr != syscall.Errno(0) {
			return fmt.Errorf("%w: could not move cursor: %v", errCursorAPI, callErr)
		}
		return fmt.Errorf("%w: could not move cursor", errCursorAPI)
	}
	time.Sleep(virtualClickMoveSettle)

	// Calculate lParam for mouse messages
	messageHWND, messageX, messageY := resolveMouseMessageTarget(hwnd, x, y)
	if messageHWND == 0 || messageX < 0 || messageY < 0 {
		procClipCursor.Call(uintptr(unsafe.Pointer(&origClip)))
		return fmt.Errorf("click point could not be mapped to the selected window")
	}
	lParam := uintptr(uint16(messageX)) | uintptr(uint16(messageY))<<16

	// Send click via SendMessage
	send := func(message uint32, keyState uintptr) error {
		var result uintptr
		ok, _, callErr := procSendMessageTimeoutW.Call(
			messageHWND, uintptr(message), keyState, lParam,
			smtoAbortIfHung, uintptr(virtualClickMessageTimeout.Milliseconds()), uintptr(unsafe.Pointer(&result)),
		)
		if ok == 0 {
			if callErr != syscall.Errno(0) {
				return fmt.Errorf("mouse message 0x%X failed: %w", message, callErr)
			}
			return fmt.Errorf("mouse message 0x%X timed out", message)
		}
		return nil
	}

	var clickErr error
	if err := send(wmMouseMove, 0); err != nil {
		clickErr = err
	} else if err := send(wmLButtonDown, mkLButton); err != nil {
		_ = send(wmLButtonUp, 0)
		clickErr = err
	} else {
		time.Sleep(virtualClickButtonHold)
		if err := send(wmLButtonUp, 0); err != nil {
			time.Sleep(virtualClickReleaseSettle)
			if retryErr := send(wmLButtonUp, 0); retryErr != nil {
				clickErr = fmt.Errorf("%v; release retry failed: %w", err, retryErr)
			} else {
				clickErr = err
			}
		}
		time.Sleep(virtualClickReleaseSettle)
	}

	// Restore original clip rect so any clip set by the game or other apps is preserved.
	procClipCursor.Call(uintptr(unsafe.Pointer(&origClip)))

	return clickErr
}

// ReleaseClipCursor releases any active cursor clip. Call this on shutdown
// or when switching target windows to ensure cursor freedom is restored.
func ReleaseClipCursor() {
	virtualClickMu.Lock()
	defer virtualClickMu.Unlock()
	procClipCursor.Call(0)
}
